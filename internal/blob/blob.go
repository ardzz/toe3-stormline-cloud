// Package blob defines storage for immutable save snapshots under server-generated keys.
package blob

import (
	"context"
	"errors"
	"regexp"
	"strings"
)

var (
	// ErrNotFound reports a key with no stored object.
	ErrNotFound = errors.New("blob: not found")
	// ErrInvalidKey reports a key outside the allowed form, e.g. one that could escape the storage root.
	ErrInvalidKey = errors.New("blob: invalid key")
)

const maxKeyLen = 512

var keyRE = regexp.MustCompile(`^[a-z0-9._-]+(/[a-z0-9._-]+)*$`)

// Store keeps immutable objects. Put replaces an existing object atomically, Get returns ErrNotFound for unknown
// keys, and deleting an unknown key succeeds. Every method rejects keys that fail CheckKey.
type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// CheckKey accepts slash-separated segments of [a-z0-9._-], none of them "." or "..", up to 512 bytes.
func CheckKey(key string) error {
	if len(key) > maxKeyLen || !keyRE.MatchString(key) {
		return ErrInvalidKey
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "." || seg == ".." {
			return ErrInvalidKey
		}
	}
	return nil
}
