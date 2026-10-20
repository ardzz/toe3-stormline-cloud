package account

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/auth"
)

const ttl = 90 * 24 * time.Hour

func newTestService(open bool) (*Service, *fakeUsers, *fakeSessions, *fakeClock) {
	c := newFakeClock()
	users, sessions := newFakeStores()
	svc := NewService(users, sessions, plainHasher{}, auth.NewBackoff(c.Now, 5, time.Second, 15*time.Minute),
		Options{RegistrationOpen: open, SessionTTL: ttl, Now: c.Now})
	return svc, users, sessions, c
}

func TestRegisterClosed(t *testing.T) {
	svc, _, _, _ := newTestService(false)
	if _, err := svc.Register(context.Background(), "alice", "correct horse battery", ""); !errors.Is(err, ErrRegistrationClosed) {
		t.Fatalf("err = %v, want ErrRegistrationClosed", err)
	}
}

func TestRegisterValidatesAndRejectsDuplicates(t *testing.T) {
	svc, _, _, _ := newTestService(true)
	ctx := context.Background()
	if _, err := svc.Register(ctx, "x", "correct horse battery", ""); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("short username: %v", err)
	}
	if _, err := svc.Register(ctx, "alice", "short", ""); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := svc.Register(ctx, "Alice", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Register(ctx, "alice", "correct horse battery", ""); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate: %v", err)
	}
}

func TestRegisterIssuesWorkingSession(t *testing.T) {
	svc, _, _, c := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "Alice", "correct horse battery", "Pixel 7")
	if err != nil {
		t.Fatal(err)
	}
	if iss.Token == "" || !iss.ExpiresAt.Equal(c.Now().Add(ttl)) {
		t.Fatalf("issued %+v", iss)
	}
	if id, err := svc.Authenticate(ctx, iss.Token); err != nil || id != iss.UserID {
		t.Fatalf("Authenticate = %d, %v; want %d", id, err, iss.UserID)
	}
}

func TestLoginFailuresAreGeneric(t *testing.T) {
	svc, _, _, _ := newTestService(true)
	ctx := context.Background()
	if _, err := svc.Register(ctx, "alice", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{{"alice", "wrong horse battery"}, {"nobody", "correct horse battery"}, {"x", "y"}} {
		if _, err := svc.Login(ctx, c[0], c[1], ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("Login(%q) err = %v, want ErrInvalidCredentials", c[0], err)
		}
	}
}

func TestLoginThrottlesFromFifthFailure(t *testing.T) {
	svc, _, _, c := newTestService(true)
	ctx := context.Background()
	if _, err := svc.Register(ctx, "alice", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := svc.Login(ctx, "alice", "wrong horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure %d: %v", i+1, err)
		}
	}
	var te *ThrottledError
	if _, err := svc.Login(ctx, "alice", "correct horse battery", ""); !errors.As(err, &te) || te.RetryAfter != time.Second {
		t.Fatalf("6th attempt err = %v, want ThrottledError{1s}", err)
	}
	c.Advance(time.Second)
	if _, err := svc.Login(ctx, "ALICE", "correct horse battery", ""); err != nil {
		t.Fatalf("login after lockout: %v", err)
	}
	if _, err := svc.Login(ctx, "alice", "wrong horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("success did not reset the throttle: %v", err)
	}
}

func TestLoginRejectsDisabledAccount(t *testing.T) {
	svc, users, _, _ := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "alice", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := users.SetDisabled(ctx, "alice", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "alice", "correct horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login of disabled account: %v", err)
	}
	if _, err := svc.Authenticate(ctx, iss.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("token of disabled account: %v", err)
	}
}

func TestAuthenticateSlidesExpiryAtMostHourly(t *testing.T) {
	svc, _, sessions, c := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "alice", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Advance(30 * time.Minute)
	if _, err := svc.Authenticate(ctx, iss.Token); err != nil || sessions.touches != 0 {
		t.Fatalf("at 30m: err %v, touches %d; want nil, 0", err, sessions.touches)
	}
	c.Advance(31 * time.Minute)
	if _, err := svc.Authenticate(ctx, iss.Token); err != nil || sessions.touches != 1 {
		t.Fatalf("at 61m: err %v, touches %d; want nil, 1", err, sessions.touches)
	}
	s, _ := sessions.ByHash(ctx, auth.HashToken(iss.Token))
	if !s.ExpiresAt.Equal(c.Now().Add(ttl)) {
		t.Fatalf("expires %v, want %v", s.ExpiresAt, c.Now().Add(ttl))
	}
}

func TestAuthenticateRejectsExpiredEmptyAndUnknownTokens(t *testing.T) {
	svc, _, _, c := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "alice", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"", "not-a-token"} {
		if _, err := svc.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthorized) {
			t.Errorf("Authenticate(%q) = %v, want ErrUnauthorized", tok, err)
		}
	}
	c.Advance(ttl)
	if _, err := svc.Authenticate(ctx, iss.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestLogoutAndPurge(t *testing.T) {
	svc, _, sessions, c := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "alice", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, "alice", "correct horse battery", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(ctx, iss.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, iss.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("token after logout: %v", err)
	}
	if err := svc.Logout(ctx, ""); err != nil {
		t.Fatalf("Logout(\"\") = %v", err)
	}
	c.Advance(ttl)
	if n, err := svc.PurgeExpiredSessions(ctx); err != nil || n != 1 || sessions.count() != 0 {
		t.Fatalf("purge = %d, %v (left %d); want 1, nil, 0", n, err, sessions.count())
	}
}

func TestLoginRetriesFailedDummyHashInitialization(t *testing.T) {
	svc, _, _, _ := newTestService(true)
	hasher := &retryHasher{}
	svc.hasher = hasher
	ctx := context.Background()
	if _, err := svc.Login(ctx, "unknown", "wrong horse battery", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("initialization failure = %v, want context.DeadlineExceeded", err)
	}
	if hasher.verifies != 0 {
		t.Errorf("failed initialization verified %d times, want 0", hasher.verifies)
	}
	for range 2 {
		if _, err := svc.Login(ctx, "unknown", "wrong horse battery", ""); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("unknown-user login = %v, want ErrInvalidCredentials", err)
		}
	}
	if hasher.hashes != 2 || hasher.verifies != 2 {
		t.Fatalf("hashes = %d, verifies = %d; want 2, 2", hasher.hashes, hasher.verifies)
	}
}

func TestAuthenticateConcurrentTouchesWriteOnce(t *testing.T) {
	svc, _, sessions, c := newTestService(true)
	ctx := context.Background()
	iss, err := svc.Register(ctx, "alice", "correct horse battery", "")
	if err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Hour)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	sessions.afterRead = func() {
		arrived <- struct{}{}
		<-release
	}
	type result struct {
		id  int64
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			id, err := svc.Authenticate(ctx, iss.Token)
			results <- result{id, err}
		}()
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for range 2 {
		select {
		case <-arrived:
		case <-timer.C:
			close(release)
			t.Fatal("concurrent authentications did not reach the read barrier")
		}
	}
	close(release)
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil || got.id != iss.UserID {
				t.Errorf("Authenticate = %d, %v; want %d, nil", got.id, got.err, iss.UserID)
			}
		case <-timer.C:
			t.Fatal("concurrent authentications did not finish")
		}
	}
	if sessions.touches != 1 {
		t.Fatalf("concurrent authentication wrote %d touches, want 1", sessions.touches)
	}
}
