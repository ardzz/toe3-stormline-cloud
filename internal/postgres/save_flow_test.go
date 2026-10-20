//go:build integration

package postgres_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/fsblob"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

func gzipBytes(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestSaveFlowOnPostgres(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, saves := postgres.NewUsers(pool), postgres.NewSaves(pool)
	blobs, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	svc := save.NewService(saves, blobs, save.Options{MaxBytes: 1 << 20, Retention: 2})
	// Sweep as if two hours had passed, beyond the hour a queued blob is held for late writes.
	sweeper := save.NewSweeper(saves, blobs, func() time.Time { return time.Now().Add(2 * time.Hour) })
	uid := newSaveUser(t, ctx, users, "alice")
	upload := func(head int64, payload string) (save.Revision, error) {
		data := gzipBytes(t, payload)
		return svc.Upload(ctx, uid, head, data, save.Meta{SHA256: sha256Hex(data), Device: "Pixel 7", Reason: "manual", GameBuild: 193})
	}

	first, err := upload(0, "one")
	if err != nil || first.Rev != 1 {
		t.Fatalf("first upload = %+v, %v", first, err)
	}
	// Two devices upload against the same head: exactly one wins, the other learns the new head.
	results := make(chan error, 2)
	for _, p := range []string{"two-a", "two-b"} {
		go func() {
			_, err := upload(1, p)
			results <- err
		}()
	}
	won, lost := 0, 0
	for range 2 {
		err := <-results
		var stale *save.StaleError
		switch {
		case err == nil:
			won++
		case errors.As(err, &stale) && stale.Latest > 1:
			lost++
		default:
			t.Fatalf("concurrent upload: %v", err)
		}
	}
	if won != 1 || lost != 1 {
		t.Fatalf("won %d, lost %d; want 1, 1", won, lost)
	}
	head, data, err := svc.Latest(ctx, uid)
	if err != nil || sha256Hex(data) != head.SHA256 {
		t.Fatalf("Latest = %+v, %v", head, err)
	}
	if _, err := upload(head.Rev, "three"); err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs.Get(ctx, first.BlobKey); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("pruned revision's blob = %v, want ErrNotFound", err)
	}
	kept, err := svc.List(ctx, uid)
	if err != nil || len(kept) != 2 {
		t.Fatalf("List = %+v, %v; want 2 revisions", kept, err)
	}
	for _, r := range kept {
		if _, _, err := svc.Get(ctx, uid, r.Rev); err != nil {
			t.Fatalf("Get(%d) = %v", r.Rev, err)
		}
	}
	if _, err := users.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := sweeper.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range kept {
		if _, err := blobs.Get(ctx, r.BlobKey); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("deleted account's blob %s = %v, want ErrNotFound", r.BlobKey, err)
		}
	}
}

func TestLateWriteOfLostReservationIsSwept(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, saves := postgres.NewUsers(pool), postgres.NewSaves(pool)
	blobs, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	uid := newSaveUser(t, ctx, users, "alice")
	r, err := saves.Reserve(ctx, uid, 0, pendingRev(time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	sweepAt := func(at time.Time) (int, error) {
		return save.NewSweeper(saves, blobs, func() time.Time { return at }).Sweep(ctx)
	}
	if n, err := sweepAt(now); err != nil || n != 0 {
		t.Fatalf("early Sweep = %d, %v; want 0, nil", n, err)
	}
	if err := blobs.Put(ctx, r.BlobKey, gzipBytes(t, "late")); err != nil {
		t.Fatal(err)
	}
	if err := saves.Commit(ctx, uid, 0, r.Rev, 20); !errors.Is(err, save.ErrReservationLost) {
		t.Fatalf("late Commit = %v; want ErrReservationLost", err)
	}
	if n, err := sweepAt(now.Add(2 * time.Hour)); err != nil || n != 1 {
		t.Fatalf("late Sweep = %d, %v; want 1, nil", n, err)
	}
	if _, err := blobs.Get(ctx, r.BlobKey); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("late blob = %v; want ErrNotFound", err)
	}
}

// stallingBlobs runs beforePut ahead of each write, standing in for an upload that stalls before publishing.
type stallingBlobs struct {
	blob.Store
	beforePut func()
}

func (s stallingBlobs) Put(ctx context.Context, key string, data []byte) error {
	s.beforePut()
	return s.Store.Put(ctx, key, data)
}

func TestLateWriteAfterSweepIsQueuedAgain(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, saves := postgres.NewUsers(pool), postgres.NewSaves(pool)
	blobs, err := fsblob.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { blobs.Close() })
	uid := newSaveUser(t, ctx, users, "alice")
	key := save.BlobKey(uid, 1)
	sweepAt := func(at time.Time) (int, error) {
		return save.NewSweeper(saves, blobs, func() time.Time { return at }).Sweep(ctx)
	}
	var deleteErr, sweepErr error
	var swept int
	beforePut := func() {
		_, deleteErr = users.Delete(ctx, "alice")
		swept, sweepErr = sweepAt(time.Now().Add(2 * time.Hour))
	}
	svc := save.NewService(saves, stallingBlobs{Store: blobs, beforePut: beforePut},
		save.Options{MaxBytes: 1 << 20, Retention: 20})
	data := gzipBytes(t, "late")
	_, err = svc.Upload(ctx, uid, 0, data, save.Meta{SHA256: sha256Hex(data), Reason: "manual"})
	if deleteErr != nil || sweepErr != nil || swept != 1 {
		t.Fatalf("beforePut: Delete = %v; Sweep = %d, %v; want nil, 1, nil", deleteErr, swept, sweepErr)
	}
	if !errors.Is(err, save.ErrReservationLost) {
		t.Fatalf("Upload = %v; want ErrReservationLost", err)
	}
	if got, err := blobs.Get(ctx, key); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("late blob = %q, %v; want uploaded bytes", got, err)
	}
	if keys := garbageKeys(t, ctx, saves); len(keys) != 1 || keys[0] != key {
		t.Fatalf("garbage = %v; want [%s]", keys, key)
	}
	if n, err := sweepAt(time.Now().Add(2 * time.Hour)); err != nil || n != 1 {
		t.Fatalf("final Sweep = %d, %v; want 1, nil", n, err)
	}
	if _, err := blobs.Get(ctx, key); !errors.Is(err, blob.ErrNotFound) {
		t.Fatalf("swept blob = %v; want ErrNotFound", err)
	}
}
