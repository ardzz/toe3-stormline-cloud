package account

import (
	"context"
	"sync"
	"time"
)

type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock               { return &fakeClock{t: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

// plainHasher is a fast stand-in for auth.Hasher in unit tests.
type plainHasher struct{}

func (plainHasher) Hash(_ context.Context, pw string) (string, error) { return "plain:" + pw, nil }
func (plainHasher) Verify(_ context.Context, pw, enc string) (bool, error) {
	return enc == "plain:"+pw, nil
}

// fakeDB is the shared state behind fakeUsers and fakeSessions. One mutex guards users and sessions together so the
// conditional session create and the revoke-on-change updates are atomic, as in the PostgreSQL stores.
type fakeDB struct {
	mu        sync.Mutex
	nextID    int64
	byName    map[string]*User
	byHash    map[string]Session
	touches   int
	afterRead func()
}

// fakeUsers is the account.UserAdmin view of a fakeDB.
type fakeUsers struct{ *fakeDB }

// fakeSessions is the account.Sessions view of a fakeDB.
type fakeSessions struct{ *fakeDB }

func newFakeStores() (*fakeUsers, *fakeSessions) {
	db := &fakeDB{byName: map[string]*User{}, byHash: map[string]Session{}}
	return &fakeUsers{db}, &fakeSessions{db}
}

// userByID must be called with mu held.
func (db *fakeDB) userByID(id int64) (*User, bool) {
	for _, u := range db.byName {
		if u.ID == id {
			return u, true
		}
	}
	return nil, false
}

// revoke deletes matching sessions and must be called with mu held.
func (db *fakeDB) revoke(match func(Session) bool) int64 {
	var n int64
	for k, s := range db.byHash {
		if match(s) {
			delete(db.byHash, k)
			n++
		}
	}
	return n
}

func (db *fakeDB) count() int {
	db.mu.Lock()
	defer db.mu.Unlock()
	return len(db.byHash)
}

func (f *fakeUsers) Create(_ context.Context, username, pwHash string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byName[username]; ok {
		return 0, ErrUsernameTaken
	}
	f.nextID++
	f.byName[username] = &User{ID: f.nextID, Username: username, PwHash: pwHash}
	return f.nextID, nil
}

func (f *fakeUsers) ByUsername(_ context.Context, username string) (User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byName[username]
	if !ok {
		return User{}, ErrNotFound
	}
	return *u, nil
}

// update applies fn to one user and, if revoke is set, deletes that user's sessions in the same critical section.
func (f *fakeUsers) update(username string, revoke bool, fn func(*User)) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byName[username]
	if !ok {
		return 0, ErrNotFound
	}
	fn(u)
	if revoke {
		f.revoke(func(s Session) bool { return s.UserID == u.ID })
	}
	return u.ID, nil
}

func (f *fakeUsers) SetPassword(_ context.Context, username, pwHash string) (int64, error) {
	return f.update(username, true, func(u *User) { u.PwHash = pwHash })
}

func (f *fakeUsers) SetDisabled(_ context.Context, username string, disabled bool) (int64, error) {
	return f.update(username, disabled, func(u *User) { u.Disabled = disabled })
}

func (f *fakeUsers) Delete(_ context.Context, username string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byName[username]
	if !ok {
		return 0, ErrNotFound
	}
	delete(f.byName, username)
	f.revoke(func(s Session) bool { return s.UserID == u.ID })
	return u.ID, nil
}

func (f *fakeSessions) Create(_ context.Context, s Session, pwHash string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.userByID(s.UserID); !ok || u.Disabled || u.PwHash != pwHash {
		return false, nil
	}
	f.byHash[string(s.TokenHash)] = s
	return true, nil
}

// ByHash mirrors the PostgreSQL store, hiding sessions of disabled accounts; afterRead runs after the read.
func (f *fakeSessions) ByHash(_ context.Context, hash []byte) (Session, error) {
	f.mu.Lock()
	s, ok := f.byHash[string(hash)]
	if ok {
		u, found := f.userByID(s.UserID)
		ok = found && !u.Disabled
	}
	f.mu.Unlock()
	if f.afterRead != nil {
		f.afterRead()
	}
	if !ok {
		return Session{}, ErrNotFound
	}
	return s, nil
}

func (f *fakeSessions) Touch(_ context.Context, hash []byte, staleBefore, lastUsed, expires time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.byHash[string(hash)]; ok && !s.LastUsedAt.After(staleBefore) {
		s.LastUsedAt, s.ExpiresAt = lastUsed, expires
		f.byHash[string(hash)] = s
		f.touches++
		return true, nil
	}
	return false, nil
}

func (f *fakeSessions) Delete(_ context.Context, hash []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.byHash, string(hash))
	return nil
}

func (f *fakeSessions) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revoke(func(s Session) bool { return !now.Before(s.ExpiresAt) }), nil
}

type retryHasher struct {
	plainHasher
	hashes   int
	verifies int
}

func (h *retryHasher) Hash(ctx context.Context, password string) (string, error) {
	h.hashes++
	if h.hashes == 1 {
		return "", context.DeadlineExceeded
	}
	return h.plainHasher.Hash(ctx, password)
}

func (h *retryHasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	h.verifies++
	return h.plainHasher.Verify(ctx, password, encoded)
}

// gateHasher signals entered when Verify starts and blocks it until release is closed.
type gateHasher struct {
	plainHasher
	entered chan struct{}
	release chan struct{}
}

func (h *gateHasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	h.entered <- struct{}{}
	<-h.release
	return h.plainHasher.Verify(ctx, password, encoded)
}
