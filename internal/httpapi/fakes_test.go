package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
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

// fakeSaves returns preset results and records calls and the last upload.
type fakeSaves struct {
	revs     []save.Revision
	rev      save.Revision // from Upload, Latest and Get
	data     []byte
	err      error
	calls    []string
	uploaded []byte
	meta     save.Meta
}

func (f *fakeSaves) Upload(_ context.Context, userID, expectedHead int64, data []byte, m save.Meta) (save.Revision, error) {
	f.calls = append(f.calls, fmt.Sprintf("upload %d head=%d", userID, expectedHead))
	f.uploaded, f.meta = data, m
	return f.rev, f.err
}

func (f *fakeSaves) List(_ context.Context, userID int64) ([]save.Revision, error) {
	f.calls = append(f.calls, fmt.Sprintf("list %d", userID))
	return f.revs, f.err
}

func (f *fakeSaves) Latest(_ context.Context, userID int64) (save.Revision, []byte, error) {
	f.calls = append(f.calls, fmt.Sprintf("latest %d", userID))
	return f.rev, f.data, f.err
}

func (f *fakeSaves) Get(_ context.Context, userID, rev int64) (save.Revision, []byte, error) {
	f.calls = append(f.calls, fmt.Sprintf("get %d %d", userID, rev))
	return f.rev, f.data, f.err
}
