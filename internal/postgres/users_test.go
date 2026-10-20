//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

func TestUsersStore(t *testing.T) {
	ctx := context.Background()
	users := postgres.NewUsers(pgtest.Pool(t))
	id, err := users.Create(ctx, "alice", "hash-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.Create(ctx, "alice", "hash-2"); !errors.Is(err, account.ErrUsernameTaken) {
		t.Fatalf("duplicate: %v", err)
	}
	if u, err := users.ByUsername(ctx, "alice"); err != nil || u != (account.User{ID: id, Username: "alice", PwHash: "hash-1"}) {
		t.Fatalf("ByUsername = %+v, %v", u, err)
	}
	if _, err := users.ByUsername(ctx, "ghost"); !errors.Is(err, account.ErrNotFound) {
		t.Fatalf("ghost: %v", err)
	}
	if got, err := users.SetPassword(ctx, "alice", "hash-3"); err != nil || got != id {
		t.Fatalf("SetPassword = %d, %v", got, err)
	}
	if got, err := users.SetDisabled(ctx, "alice", true); err != nil || got != id {
		t.Fatalf("SetDisabled = %d, %v", got, err)
	}
	if u, _ := users.ByUsername(ctx, "alice"); !u.Disabled || u.PwHash != "hash-3" {
		t.Fatalf("after updates: %+v", u)
	}
	if _, err := users.SetDisabled(ctx, "alice", false); err != nil {
		t.Fatal(err)
	}
	if u, _ := users.ByUsername(ctx, "alice"); u.Disabled {
		t.Fatal("still disabled after enable")
	}
	for name, fn := range map[string]func() (int64, error){
		"SetPassword": func() (int64, error) { return users.SetPassword(ctx, "ghost", "h") },
		"SetDisabled": func() (int64, error) { return users.SetDisabled(ctx, "ghost", true) },
		"Delete":      func() (int64, error) { return users.Delete(ctx, "ghost") },
	} {
		if _, err := fn(); !errors.Is(err, account.ErrNotFound) {
			t.Errorf("%s(ghost) = %v, want ErrNotFound", name, err)
		}
	}
	if got, err := users.Delete(ctx, "alice"); err != nil || got != id {
		t.Fatalf("Delete = %d, %v", got, err)
	}
}
