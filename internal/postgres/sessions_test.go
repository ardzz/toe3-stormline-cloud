//go:build integration

package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

var sessionNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func newSession(b byte, uid int64, exp time.Time) account.Session {
	return account.Session{TokenHash: bytes.Repeat([]byte{b}, 32), UserID: uid, Device: "Pixel",
		CreatedAt: sessionNow, LastUsedAt: sessionNow, ExpiresAt: exp}
}

func countSessions(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func mustCreate(t *testing.T, sessions *postgres.Sessions, s account.Session, pwHash string) {
	t.Helper()
	if stored, err := sessions.Create(context.Background(), s, pwHash); err != nil || !stored {
		t.Fatalf("Create = %v, %v; want true", stored, err)
	}
}

func TestSessionsStore(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, sessions := postgres.NewUsers(pool), postgres.NewSessions(pool)
	uid, err := users.Create(ctx, "alice", "h")
	if err != nil {
		t.Fatal(err)
	}
	now := sessionNow
	a, b := newSession(1, uid, now.Add(time.Hour)), newSession(2, uid, now.Add(time.Minute))
	mustCreate(t, sessions, a, "h")
	mustCreate(t, sessions, b, "h")
	got, err := sessions.ByHash(ctx, a.TokenHash)
	if err != nil || !bytes.Equal(got.TokenHash, a.TokenHash) || got.UserID != uid || got.Device != "Pixel" ||
		!got.CreatedAt.Equal(now) || !got.LastUsedAt.Equal(now) || !got.ExpiresAt.Equal(a.ExpiresAt) {
		t.Fatalf("ByHash = %+v, %v", got, err)
	}
	later := now.Add(2 * time.Hour)
	if wrote, err := sessions.Touch(ctx, a.TokenHash, later.Add(-time.Hour), later, later.Add(time.Hour)); err != nil || !wrote {
		t.Fatalf("stale Touch = %v, %v; want true", wrote, err)
	}
	if got, _ := sessions.ByHash(ctx, a.TokenHash); !got.LastUsedAt.Equal(later) || !got.ExpiresAt.Equal(later.Add(time.Hour)) {
		t.Fatalf("after Touch: %+v", got)
	}
	if wrote, err := sessions.Touch(ctx, a.TokenHash, later.Add(-time.Hour), later.Add(time.Minute), later.Add(2*time.Hour)); err != nil || wrote {
		t.Fatalf("fresh Touch = %v, %v; want false", wrote, err)
	}
	stale := later.Add(2 * time.Hour)
	results := make(chan bool, 2)
	for range 2 {
		go func() {
			wrote, err := sessions.Touch(ctx, a.TokenHash, stale.Add(-time.Hour), stale, stale.Add(time.Hour))
			if err != nil {
				t.Error(err)
			}
			results <- wrote
		}()
	}
	if w1, w2 := <-results, <-results; w1 == w2 {
		t.Fatalf("concurrent stale touches wrote %v and %v; want exactly one", w1, w2)
	}
	if n, err := sessions.DeleteExpired(ctx, now.Add(time.Minute)); err != nil || n != 1 {
		t.Fatalf("DeleteExpired = %d, %v; want 1", n, err)
	}
	if _, err := sessions.ByHash(ctx, b.TokenHash); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("expired session still present: %v", err)
	}
	if err := sessions.Delete(ctx, a.TokenHash); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.ByHash(ctx, a.TokenHash); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("deleted session visible: %v", err)
	}
	c := newSession(5, uid, now.Add(time.Hour))
	mustCreate(t, sessions, c, "h")
	// Disable behind the store's back, so the session row remains and only ByHash's filter hides it.
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = $1`, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.ByHash(ctx, c.TokenHash); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("disabled account's session visible: %v", err)
	}
	if _, err := users.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if left := countSessions(t, pool); left != 0 {
		t.Fatalf("sessions after user delete = %d; want 0", left)
	}
}

func TestSessionsCreateRequiresCurrentCredentials(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, sessions := postgres.NewUsers(pool), postgres.NewSessions(pool)
	uid, err := users.Create(ctx, "alice", "h1")
	if err != nil {
		t.Fatal(err)
	}
	exp := sessionNow.Add(time.Hour)
	if stored, err := sessions.Create(ctx, newSession(1, uid, exp), "stale"); err != nil || stored {
		t.Fatalf("Create with stale hash = %v, %v; want false", stored, err)
	}
	mustCreate(t, sessions, newSession(2, uid, exp), "h1")
	if _, err := users.SetDisabled(ctx, "alice", false); err != nil {
		t.Fatal(err)
	}
	if n := countSessions(t, pool); n != 1 {
		t.Fatalf("SetDisabled(false) left %d sessions; want 1", n)
	}
	if _, err := users.SetDisabled(ctx, "alice", true); err != nil {
		t.Fatal(err)
	}
	if n := countSessions(t, pool); n != 0 {
		t.Fatalf("SetDisabled(true) left %d sessions; want 0", n)
	}
	if stored, err := sessions.Create(ctx, newSession(3, uid, exp), "h1"); err != nil || stored {
		t.Fatalf("Create on disabled account = %v, %v; want false", stored, err)
	}
	if _, err := users.SetDisabled(ctx, "alice", false); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, sessions, newSession(4, uid, exp), "h1")
	if _, err := users.SetPassword(ctx, "alice", "h2"); err != nil {
		t.Fatal(err)
	}
	if n := countSessions(t, pool); n != 0 {
		t.Fatalf("SetPassword left %d sessions; want 0", n)
	}
	if stored, err := sessions.Create(ctx, newSession(5, uid, exp), "h1"); err != nil || stored {
		t.Fatalf("Create with replaced hash = %v, %v; want false", stored, err)
	}
	mustCreate(t, sessions, newSession(6, uid, exp), "h2")
}

// TestSessionsCreateRacesPasswordChange checks that a session created with the old hash never survives a
// concurrent password change, whichever transaction wins the row lock.
func TestSessionsCreateRacesPasswordChange(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, sessions := postgres.NewUsers(pool), postgres.NewSessions(pool)
	uid, err := users.Create(ctx, "alice", "seed")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 20 {
		oldHash, newHash := fmt.Sprintf("old-%d", i), fmt.Sprintf("new-%d", i)
		if _, err := users.SetPassword(ctx, "alice", oldHash); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		go func() {
			<-start
			_, err := users.SetPassword(ctx, "alice", newHash)
			errs <- err
		}()
		go func() {
			<-start
			_, err := sessions.Create(ctx, newSession(byte(i), uid, sessionNow.Add(time.Hour)), oldHash)
			errs <- err
		}()
		close(start)
		for range 2 {
			if err := <-errs; err != nil {
				t.Fatalf("iteration %d: %v", i, err)
			}
		}
		if n := countSessions(t, pool); n != 0 {
			t.Fatalf("iteration %d: %d sessions survived the password change; want 0", i, n)
		}
	}
}
