package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

// fakeAccounts returns preset results and records calls.
type fakeAccounts struct {
	issued  account.Issued
	err     error // from Register, Login and Logout
	userID  int64
	authErr error
	calls   []string
}

func (f *fakeAccounts) Register(_ context.Context, username, password, device string) (account.Issued, error) {
	f.calls = append(f.calls, fmt.Sprintf("register %s %s %s", username, password, device))
	return f.issued, f.err
}

func (f *fakeAccounts) Login(_ context.Context, username, password, device string) (account.Issued, error) {
	f.calls = append(f.calls, fmt.Sprintf("login %s %s %s", username, password, device))
	return f.issued, f.err
}

func (f *fakeAccounts) Logout(_ context.Context, token string) error {
	f.calls = append(f.calls, "logout "+token)
	return f.err
}

func (f *fakeAccounts) Authenticate(_ context.Context, token string) (int64, error) {
	f.calls = append(f.calls, "auth "+token)
	return f.userID, f.authErr
}

// fakeLimiter returns a preset wait and records keys.
type fakeLimiter struct {
	wait time.Duration
	keys []string
}

func (f *fakeLimiter) Allow(key string) time.Duration {
	f.keys = append(f.keys, key)
	return f.wait
}
