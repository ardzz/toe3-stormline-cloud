package account

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	minPasswordRunes = 10
	maxPasswordRunes = 256
	maxDeviceRunes   = 64
)

var usernameRE = regexp.MustCompile(`^[a-z0-9_.-]{3,32}$`)

// NormalizeUsername trims and lowercases a username and checks it against ^[a-z0-9_.-]{3,32}$.
func NormalizeUsername(s string) (string, error) {
	u := strings.ToLower(strings.TrimSpace(s))
	if !usernameRE.MatchString(u) {
		return "", ErrInvalidUsername
	}
	return u, nil
}

// ValidatePassword accepts 10-256 Unicode characters.
func ValidatePassword(p string) error {
	if n := utf8.RuneCountInString(p); n < minPasswordRunes || n > maxPasswordRunes {
		return ErrInvalidPassword
	}
	return nil
}
