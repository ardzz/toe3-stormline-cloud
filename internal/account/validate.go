package account

import (
	"regexp"
	"strings"
	"unicode"
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

// cleanDevice drops control characters and limits a client-supplied device label to 64 characters.
func cleanDevice(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > maxDeviceRunes {
		s = string(r[:maxDeviceRunes])
	}
	return s
}
