package account

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrNotFound           = errors.New("account: not found")
	ErrUsernameTaken      = errors.New("account: username taken")
	ErrInvalidUsername    = errors.New("account: username must match ^[a-z0-9_.-]{3,32}$")
	ErrInvalidPassword    = errors.New("account: password must be 10-256 characters")
	ErrRegistrationClosed = errors.New("account: registration is closed")
	ErrInvalidCredentials = errors.New("account: invalid credentials")
	ErrUnauthorized       = errors.New("account: invalid or expired session")
)

// ThrottledError refuses a login until RetryAfter has elapsed.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("account: too many failed logins; retry after %s", e.RetryAfter)
}
