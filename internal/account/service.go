package account

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/auth"
	"github.com/ardzz/toe3-stormline-cloud/internal/label"
)

// touchInterval bounds how often authentication rewrites a session's sliding expiry.
const touchInterval = time.Hour

// Options configures a Service.
type Options struct {
	RegistrationOpen bool
	SessionTTL       time.Duration
	Now              func() time.Time // defaults to time.Now
}

// Issued is a new session. Token is returned to the client once and never stored.
type Issued struct {
	UserID    int64
	Token     string
	ExpiresAt time.Time
}

// Service implements the player-facing account flows.
type Service struct {
	users    Users
	sessions Sessions
	hasher   PasswordHasher
	throttle Throttle
	opts     Options

	dummyMu   sync.Mutex
	dummyHash string
}

// NewService wires a Service from its stores and policies.
func NewService(users Users, sessions Sessions, hasher PasswordHasher, throttle Throttle, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{users: users, sessions: sessions, hasher: hasher, throttle: throttle, opts: opts}
}

// Register creates an account and logs it in; it requires open registration.
func (s *Service) Register(ctx context.Context, username, password, device string) (Issued, error) {
	if !s.opts.RegistrationOpen {
		return Issued{}, ErrRegistrationClosed
	}
	id, pwHash, err := createUser(ctx, s.users, s.hasher, username, password)
	if err != nil {
		return Issued{}, err
	}
	return s.issue(ctx, id, pwHash, device)
}

// Login verifies credentials and issues a session. Every credential problem yields ErrInvalidCredentials.
func (s *Service) Login(ctx context.Context, username, password, device string) (Issued, error) {
	u, err := NormalizeUsername(username)
	if err != nil {
		return Issued{}, ErrInvalidCredentials
	}
	if wait := s.throttle.Allow(u); wait > 0 {
		return Issued{}, &ThrottledError{RetryAfter: wait}
	}
	user, err := s.users.ByUsername(ctx, u)
	if errors.Is(err, ErrNotFound) {
		if err := s.burnHash(ctx, password); err != nil {
			return Issued{}, err
		}
		s.throttle.Fail(u)
		return Issued{}, ErrInvalidCredentials
	}
	if err != nil {
		return Issued{}, err
	}
	ok, err := s.hasher.Verify(ctx, password, user.PwHash)
	if err != nil {
		return Issued{}, err
	}
	if !ok || user.Disabled {
		s.throttle.Fail(u)
		return Issued{}, ErrInvalidCredentials
	}
	s.throttle.Succeed(u)
	return s.issue(ctx, user.ID, user.PwHash, device)
}

// Authenticate resolves a bearer token to a user ID, sliding its expiry at most once per touchInterval.
func (s *Service) Authenticate(ctx context.Context, token string) (int64, error) {
	if token == "" {
		return 0, ErrUnauthorized
	}
	hash := auth.HashToken(token)
	sess, err := s.sessions.ByHash(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return 0, ErrUnauthorized
	}
	if err != nil {
		return 0, err
	}
	now := s.opts.Now()
	if !now.Before(sess.ExpiresAt) {
		return 0, ErrUnauthorized
	}
	if now.Sub(sess.LastUsedAt) >= touchInterval {
		if _, err := s.sessions.Touch(ctx, hash, now.Add(-touchInterval), now, now.Add(s.opts.SessionTTL)); err != nil {
			return 0, err
		}
	}
	return sess.UserID, nil
}

// Logout revokes the session behind token; unknown or empty tokens are a no-op.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.sessions.Delete(ctx, auth.HashToken(token))
}

// PurgeExpiredSessions deletes sessions whose expiry has passed and returns how many were removed.
func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.sessions.DeleteExpired(ctx, s.opts.Now())
}

// issue stores a new session only while pwHash is still the account's password hash and the account is enabled,
// so a password change or disable that lands after verification wins; otherwise it returns ErrInvalidCredentials.
func (s *Service) issue(ctx context.Context, userID int64, pwHash, device string) (Issued, error) {
	token, hash, err := auth.NewToken()
	if err != nil {
		return Issued{}, err
	}
	now := s.opts.Now()
	exp := now.Add(s.opts.SessionTTL)
	stored, err := s.sessions.Create(ctx, Session{TokenHash: hash, UserID: userID, Device: label.Clean(device, maxDeviceRunes),
		CreatedAt: now, LastUsedAt: now, ExpiresAt: exp}, pwHash)
	if err != nil {
		return Issued{}, err
	}
	if !stored {
		return Issued{}, ErrInvalidCredentials
	}
	return Issued{UserID: userID, Token: token, ExpiresAt: exp}, nil
}

// burnHash spends one verification against a fixed hash so unknown usernames cost as much as wrong passwords.
func (s *Service) burnHash(ctx context.Context, password string) error {
	s.dummyMu.Lock()
	if s.dummyHash == "" {
		h, err := s.hasher.Hash(ctx, "toe3-cloud timing equalizer")
		if err != nil {
			s.dummyMu.Unlock()
			return err
		}
		s.dummyHash = h
	}
	hash := s.dummyHash
	s.dummyMu.Unlock()
	_, _ = s.hasher.Verify(ctx, password, hash) // only the cost matters, not the result
	return nil
}

// createUser validates and hashes credentials and stores a new account; it returns the ID and the stored hash.
func createUser(ctx context.Context, users Users, hasher PasswordHasher, username, password string) (int64, string, error) {
	u, err := NormalizeUsername(username)
	if err != nil {
		return 0, "", err
	}
	if err := ValidatePassword(password); err != nil {
		return 0, "", err
	}
	h, err := hasher.Hash(ctx, password)
	if err != nil {
		return 0, "", err
	}
	id, err := users.Create(ctx, u, h)
	if err != nil {
		return 0, "", err
	}
	return id, h, nil
}
