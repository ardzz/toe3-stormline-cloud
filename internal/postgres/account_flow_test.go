//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/auth"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

func TestAccountFlowOnPostgres(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	users, sessions := postgres.NewUsers(pool), postgres.NewSessions(pool)
	hasher := auth.NewHasher(2)
	svc := account.NewService(users, sessions, hasher, auth.NewBackoff(time.Now, 5, time.Second, 15*time.Minute),
		account.Options{RegistrationOpen: true, SessionTTL: 2 * time.Hour})
	op := account.NewOperator(users, hasher)

	iss, err := svc.Register(ctx, "Alice", "correct horse battery", "Pixel 7")
	if err != nil {
		t.Fatal(err)
	}
	if id, err := svc.Authenticate(ctx, iss.Token); err != nil || id != iss.UserID {
		t.Fatalf("Authenticate = %d, %v", id, err)
	}
	if err := op.SetPassword(ctx, "alice", "another long secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, iss.Token); !errors.Is(err, account.ErrUnauthorized) {
		t.Fatalf("token after password change: %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "correct horse battery", ""); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Fatalf("login with old password: %v", err)
	}
	tablet, err := svc.Login(ctx, "alice", "another long secret", "Tablet")
	if err != nil {
		t.Fatal(err)
	}
	if err := op.SetDisabled(ctx, "alice", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, tablet.Token); !errors.Is(err, account.ErrUnauthorized) {
		t.Fatalf("token after disable: %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "another long secret", ""); !errors.Is(err, account.ErrInvalidCredentials) {
		t.Fatalf("login after disable: %v", err)
	}
	if n := countSessions(t, pool); n != 0 {
		t.Fatalf("sessions after disable: %d; want 0", n)
	}
	if err := op.SetDisabled(ctx, "alice", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "alice", "another long secret", ""); err != nil {
		t.Fatalf("login after enable: %v", err)
	}
	if err := op.Delete(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if n := countSessions(t, pool); n != 0 {
		t.Fatalf("sessions after delete: %d; want 0", n)
	}
}
