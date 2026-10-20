//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

func newSaveUser(t *testing.T, ctx context.Context, users *postgres.Users, name string) int64 {
	t.Helper()
	id, err := users.Create(ctx, name, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func pendingRev(at time.Time) save.Revision {
	return save.Revision{CreatedAt: at, Size: 10, SHA256: strings.Repeat("a", 64), Device: "Pixel", Reason: "manual", GameBuild: 193}
}

// sameRevision compares revisions with their timestamps normalised to UTC.
func sameRevision(a, b save.Revision) bool {
	a.CreatedAt, b.CreatedAt = a.CreatedAt.UTC(), b.CreatedAt.UTC()
	return a == b
}

func garbageKeys(t *testing.T, ctx context.Context, saves *postgres.Saves) []string {
	t.Helper()
	keys, err := saves.GarbageKeys(ctx, time.Now().Add(time.Hour), 1000) // every key queued so far
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(keys)
	return keys
}

func TestSavesReserveCommitAndRead(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

	r1, err := saves.Reserve(ctx, uid, 0, pendingRev(now))
	if err != nil || r1.Rev != 1 || r1.BlobKey != save.BlobKey(uid, 1) {
		t.Fatalf("Reserve = %+v, %v", r1, err)
	}
	if _, err := saves.Head(ctx, uid); !errors.Is(err, save.ErrNoSaves) {
		t.Fatalf("Head with only a pending revision = %v, want ErrNoSaves", err)
	}
	if err := saves.Commit(ctx, uid, 0, 1, 20); err != nil {
		t.Fatal(err)
	}
	if head, err := saves.Head(ctx, uid); err != nil || !sameRevision(head, r1) {
		t.Fatalf("Head = %+v, %v; want %+v", head, err, r1)
	}
	if r, err := saves.ByRev(ctx, uid, 1); err != nil || !sameRevision(r, r1) {
		t.Fatalf("ByRev(1) = %+v, %v", r, err)
	}
	if _, err := saves.ByRev(ctx, uid, 2); !errors.Is(err, save.ErrNotFound) {
		t.Fatalf("ByRev(2) = %v, want ErrNotFound", err)
	}
	var stale *save.StaleError
	if _, err := saves.Reserve(ctx, uid, 0, pendingRev(now)); !errors.As(err, &stale) || stale.Latest != 1 {
		t.Fatalf("Reserve on old head = %v, want StaleError{1}", err)
	}
	if r2, err := saves.Reserve(ctx, uid, 1, pendingRev(now.Add(time.Minute))); err != nil || r2.Rev != 2 {
		t.Fatalf("Reserve(head 1) = %+v, %v", r2, err)
	}
	if err := saves.Commit(ctx, uid, 1, 2, 20); err != nil {
		t.Fatal(err)
	}
	list, err := saves.List(ctx, uid)
	if err != nil || len(list) != 2 || list[0].Rev != 2 || list[1].Rev != 1 {
		t.Fatalf("List = %+v, %v; want revisions 2, 1", list, err)
	}
}

func TestSavesConcurrentUploadsOnSameHead(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	now := time.Now().UTC()
	a, err := saves.Reserve(ctx, uid, 0, pendingRev(now))
	if err != nil {
		t.Fatal(err)
	}
	b, err := saves.Reserve(ctx, uid, 0, pendingRev(now))
	if err != nil || b.Rev != 2 {
		t.Fatalf("second Reserve on head 0 = %+v, %v", b, err)
	}
	if err := saves.Commit(ctx, uid, 0, a.Rev, 20); err != nil {
		t.Fatal(err)
	}
	var stale *save.StaleError
	if err := saves.Commit(ctx, uid, 0, b.Rev, 20); !errors.As(err, &stale) || stale.Latest != a.Rev {
		t.Fatalf("losing Commit = %v, want StaleError{%d}", err, a.Rev)
	}
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, []string{b.BlobKey}) {
		t.Fatalf("garbage = %v, want [%s]", got, b.BlobKey)
	}
	if head, err := saves.Head(ctx, uid); err != nil || head.Rev != a.Rev {
		t.Fatalf("Head = %+v, %v", head, err)
	}
}

func TestSavesRetentionQueuesOldBlobs(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	for head := int64(0); head < 4; head++ {
		r, err := saves.Reserve(ctx, uid, head, pendingRev(time.Now().UTC()))
		if err != nil {
			t.Fatal(err)
		}
		if err := saves.Commit(ctx, uid, head, r.Rev, 2); err != nil {
			t.Fatal(err)
		}
	}
	list, err := saves.List(ctx, uid)
	if err != nil || len(list) != 2 || list[0].Rev != 4 || list[1].Rev != 3 {
		t.Fatalf("List = %+v, %v; want revisions 4, 3", list, err)
	}
	want := []string{save.BlobKey(uid, 1), save.BlobKey(uid, 2)}
	slices.Sort(want)
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, want) {
		t.Fatalf("garbage = %v, want %v", got, want)
	}
}

func TestSavesAbortExpireAndLostReservation(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	now := time.Now().UTC().Truncate(time.Microsecond)

	r1, err := saves.Reserve(ctx, uid, 0, pendingRev(now))
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.Abort(ctx, uid, r1.Rev); err != nil {
		t.Fatal(err)
	}
	if err := saves.Abort(ctx, uid, 99); err != nil {
		t.Fatalf("Abort(unknown) = %v, want nil", err)
	}
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, []string{r1.BlobKey}) {
		t.Fatalf("garbage after Abort = %v", got)
	}
	if err := saves.ForgetGarbage(ctx, r1.BlobKey); err != nil {
		t.Fatal(err)
	}
	if got := garbageKeys(t, ctx, saves); len(got) != 0 {
		t.Fatalf("garbage after Forget = %v", got)
	}
	r2, err := saves.Reserve(ctx, uid, 0, pendingRev(now.Add(-2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if n, err := saves.ExpirePending(ctx, now.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("ExpirePending = %d, %v; want 1", n, err)
	}
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, []string{r2.BlobKey}) {
		t.Fatalf("garbage after expiry = %v", got)
	}
	if err := saves.Commit(ctx, uid, 0, r2.Rev, 20); !errors.Is(err, save.ErrReservationLost) {
		t.Fatalf("Commit of expired revision = %v, want ErrReservationLost", err)
	}
}

func TestUsersDeleteQueuesSaveBlobs(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, saves := postgres.NewUsers(pool), postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, users, "alice")
	r1, err := saves.Reserve(ctx, uid, 0, pendingRev(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.Commit(ctx, uid, 0, r1.Rev, 20); err != nil {
		t.Fatal(err)
	}
	r2, err := saves.Reserve(ctx, uid, 1, pendingRev(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if id, err := users.Delete(ctx, "alice"); err != nil || id != uid {
		t.Fatalf("Delete = %d, %v", id, err)
	}
	want := []string{r1.BlobKey, r2.BlobKey}
	slices.Sort(want)
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, want) {
		t.Fatalf("garbage = %v, want %v", got, want)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM save_revisions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("revisions left = %d, %v", n, err)
	}
	if err := saves.Commit(ctx, uid, 1, r2.Rev, 20); !errors.Is(err, save.ErrReservationLost) {
		t.Fatalf("Commit after account deletion = %v, want ErrReservationLost", err)
	}
}

// waitForLockWait blocks until another session of this database waits on a lock.
func waitForLockWait(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no session started waiting on a lock")
}

func TestUsersDeleteWaitsForInFlightRevisionCleanup(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, saves := postgres.NewUsers(pool), postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, users, "alice")
	r, err := saves.Reserve(ctx, uid, 0, pendingRev(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// Hold the revision lock the way Abort and ExpirePending do before queueing its key.
	if _, err := tx.Exec(ctx, `DELETE FROM save_revisions WHERE user_id = $1 AND rev = $2`, uid, r.Rev); err != nil {
		t.Fatal(err)
	}
	deleted := make(chan error, 1)
	go func() {
		_, err := users.Delete(ctx, "alice")
		deleted <- err
	}()
	waitForLockWait(t, ctx, pool)
	if _, err := tx.Exec(ctx, `INSERT INTO blob_garbage (blob_key) VALUES ($1) ON CONFLICT DO NOTHING`, r.BlobKey); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-deleted:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("account deletion did not finish")
	}
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, []string{r.BlobKey}) {
		t.Fatalf("garbage = %v, want [%s]", got, r.BlobKey)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE username = 'alice'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("accounts left = %d, %v; want 0", n, err)
	}
}

func TestExpirePendingSkipsLockedRevisions(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	now := time.Now().UTC()
	r1, err := saves.Reserve(ctx, uid, 0, pendingRev(now.Add(-2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := saves.Reserve(ctx, uid, 0, pendingRev(now.Add(-2*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	// An upload committing r1 holds its row.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM save_revisions WHERE user_id = $1 AND rev = $2 FOR UPDATE`, uid, r1.Rev); err != nil {
		t.Fatal(err)
	}
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if n, err := saves.ExpirePending(tctx, now.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("ExpirePending with locked revision = %d, %v; want 1, nil", n, err)
	}
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, []string{r2.BlobKey}) {
		t.Fatalf("garbage = %v, want [%s]", got, r2.BlobKey)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n, err := saves.ExpirePending(ctx, now.Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("ExpirePending after unlocking = %d, %v; want 1, nil", n, err)
	}
	want := []string{r1.BlobKey, r2.BlobKey}
	slices.Sort(want)
	if got := garbageKeys(t, ctx, saves); !slices.Equal(got, want) {
		t.Fatalf("garbage = %v, want %v", got, want)
	}
}

func TestGarbageKeysHoldsRecentlyQueuedKeys(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	saves := postgres.NewSaves(pool)
	uid := newSaveUser(t, ctx, postgres.NewUsers(pool), "alice")
	r, err := saves.Reserve(ctx, uid, 0, pendingRev(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if err := saves.Abort(ctx, uid, r.Rev); err != nil {
		t.Fatal(err)
	}
	if keys, err := saves.GarbageKeys(ctx, time.Now().Add(-time.Minute), 10); err != nil || len(keys) != 0 {
		t.Fatalf("recent garbage = %v, %v; want none", keys, err)
	}
	if keys, err := saves.GarbageKeys(ctx, time.Now().Add(time.Minute), 10); err != nil || !slices.Equal(keys, []string{r.BlobKey}) {
		t.Fatalf("eligible garbage = %v, %v; want [%s]", keys, err, r.BlobKey)
	}
}
