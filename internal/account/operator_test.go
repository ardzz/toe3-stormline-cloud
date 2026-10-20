package account

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestOperator() (*Operator, *Service, *fakeSessions) {
	svc, users, sessions, _ := newTestService(false)
	return NewOperator(users, plainHasher{}), svc, sessions
}

func TestOperatorAddIgnoresRegistrationMode(t *testing.T) {
	op, svc, _ := newTestOperator()
	ctx := context.Background()
	if _, err := op.Add(ctx, "Bob", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "bob", "correct horse battery", ""); err != nil {
		t.Fatalf("login of operator-created account: %v", err)
	}
}

func TestOperatorSetPasswordRevokesSessions(t *testing.T) {
	op, svc, sessions := newTestOperator()
	ctx := context.Background()
	if _, err := op.Add(ctx, "bob", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	iss, err := svc.Login(ctx, "bob", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := op.SetPassword(ctx, "bob", "another long secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, iss.Token); !errors.Is(err, ErrUnauthorized) || sessions.count() != 0 {
		t.Fatalf("old session survived: err %v, sessions %d", err, sessions.count())
	}
	if _, err := svc.Login(ctx, "bob", "correct horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still works: %v", err)
	}
	if _, err := svc.Login(ctx, "bob", "another long secret", ""); err != nil {
		t.Fatalf("new password: %v", err)
	}
}

func TestOperatorDisableAndEnable(t *testing.T) {
	op, svc, sessions := newTestOperator()
	ctx := context.Background()
	if _, err := op.Add(ctx, "bob", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "bob", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if err := op.SetDisabled(ctx, "bob", true); err != nil {
		t.Fatal(err)
	}
	if sessions.count() != 0 {
		t.Fatalf("disable left %d sessions", sessions.count())
	}
	if _, err := svc.Login(ctx, "bob", "correct horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("disabled login: %v", err)
	}
	if err := op.SetDisabled(ctx, "bob", false); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "bob", "correct horse battery", ""); err != nil {
		t.Fatalf("re-enabled login: %v", err)
	}
}

func TestOperatorUnknownAndInvalidUsers(t *testing.T) {
	op, _, _ := newTestOperator()
	ctx := context.Background()
	if err := op.SetPassword(ctx, "ghost", "correct horse battery"); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetPassword(ghost) = %v", err)
	}
	if err := op.SetDisabled(ctx, "ghost", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetDisabled(ghost) = %v", err)
	}
	if err := op.Delete(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete(ghost) = %v", err)
	}
	if err := op.Delete(ctx, "x"); !errors.Is(err, ErrInvalidUsername) {
		t.Errorf("Delete(x) = %v", err)
	}
}

// TestLoginRacingCredentialChangeIssuesNoSession pauses Login inside Verify while the operator changes the account.
func TestLoginRacingCredentialChangeIssuesNoSession(t *testing.T) {
	for name, change := range map[string]func(context.Context, *Operator) error{
		"passwd": func(ctx context.Context, op *Operator) error {
			return op.SetPassword(ctx, "bob", "another long secret")
		},
		"disable": func(ctx context.Context, op *Operator) error { return op.SetDisabled(ctx, "bob", true) },
	} {
		t.Run(name, func(t *testing.T) {
			op, svc, sessions := newTestOperator()
			ctx := context.Background()
			if _, err := op.Add(ctx, "bob", "correct horse battery"); err != nil {
				t.Fatal(err)
			}
			gate := &gateHasher{entered: make(chan struct{}), release: make(chan struct{})}
			svc.hasher = gate
			done := make(chan error, 1)
			go func() {
				_, err := svc.Login(ctx, "bob", "correct horse battery", "")
				done <- err
			}()
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-gate.entered:
			case <-timer.C:
				t.Fatal("Login did not reach Verify")
			}
			err := change(ctx, op)
			close(gate.release)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, ErrInvalidCredentials) || sessions.count() != 0 {
					t.Fatalf("Login = %v with %d sessions; want ErrInvalidCredentials and 0", err, sessions.count())
				}
			case <-timer.C:
				t.Fatal("Login did not finish")
			}
		})
	}
}
