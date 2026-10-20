// Package account owns player identities and sessions: registration, login, logout and bearer-token authentication.
package account

import (
	"context"
	"time"
)

// User is a stored player account.
type User struct {
	ID       int64
	Username string
	PwHash   string
	Disabled bool
}

// Session is a stored login; only the SHA-256 of its bearer token is kept.
type Session struct {
	TokenHash  []byte
	UserID     int64
	Device     string
	CreatedAt  time.Time
	LastUsedAt time.Time
	ExpiresAt  time.Time
}

// Users is the user store the player flows need.
type Users interface {
	Create(ctx context.Context, username, pwHash string) (int64, error) // ErrUsernameTaken
	ByUsername(ctx context.Context, username string) (User, error)      // ErrNotFound
}

// UserAdmin adds the operator-only user operations; each returns the user ID or ErrNotFound.
// SetPassword and SetDisabled(..., true) revoke all of the account's sessions in the same atomic operation;
// SetDisabled(..., false) leaves sessions untouched.
type UserAdmin interface {
	Users
	SetPassword(ctx context.Context, username, pwHash string) (int64, error)
	SetDisabled(ctx context.Context, username string, disabled bool) (int64, error)
	Delete(ctx context.Context, username string) (int64, error)
}

// Sessions is the session store. ByHash returns ErrNotFound for unknown tokens and for disabled accounts.
type Sessions interface {
	// Create stores s only if its account is enabled and its current password hash equals pwHash, atomically with
	// respect to UserAdmin.SetPassword and SetDisabled; it reports whether it stored.
	Create(ctx context.Context, s Session, pwHash string) (bool, error)
	ByHash(ctx context.Context, hash []byte) (Session, error)
	// Touch slides a session's expiry only if it was last used at or before staleBefore, atomically; it reports whether it wrote.
	Touch(ctx context.Context, hash []byte, staleBefore, lastUsed, expires time.Time) (bool, error)
	Delete(ctx context.Context, hash []byte) error
	DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}

// PasswordHasher hashes and verifies passwords (implemented by auth.Hasher).
type PasswordHasher interface {
	Hash(ctx context.Context, password string) (string, error)
	Verify(ctx context.Context, password, encoded string) (bool, error)
}

// Throttle limits failed logins per username (implemented by auth.Backoff).
type Throttle interface {
	Allow(key string) time.Duration
	Fail(key string)
	Succeed(key string)
}
