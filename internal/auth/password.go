// Package auth holds credential primitives: password hashing, session tokens and login throttling.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: the OWASP minimum (19 MiB, 2 passes, 1 lane).
const (
	argonMemoryKiB    = 19456
	argonTime         = 2
	argonThreads      = 1
	argonSaltLen      = 16
	argonKeyLen       = 32
	maxArgonMemoryKiB = 1 << 20 // refuse stored hashes demanding more than 1 GiB
)

// ErrMalformedHash reports a stored password hash that is not a supported argon2id PHC string.
var ErrMalformedHash = errors.New("auth: malformed password hash")

// Hasher hashes and verifies passwords, running at most `limit` Argon2id computations at once.
type Hasher struct{ sem chan struct{} }

// NewHasher returns a Hasher; limit values below 1 are treated as 1.
func NewHasher(limit int) *Hasher {
	if limit < 1 {
		limit = 1
	}
	return &Hasher{sem: make(chan struct{}, limit)}
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

// Hash returns $argon2id$v=19$m=19456,t=2,p=1$<salt>$<key> with unpadded standard base64.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	h.release()
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// Verify reports whether password matches encoded, using the parameters stored in the hash.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	p, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(password), p.salt, p.time, p.memory, p.threads, uint32(len(p.key)))
	h.release()
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

type phc struct {
	memory, time uint32
	threads      uint8
	salt, key    []byte
}

func parsePHC(s string) (phc, error) {
	parts := strings.Split(s, "$") // "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return phc{}, ErrMalformedHash
	}
	if parts[2] != "v=19" {
		return phc{}, ErrMalformedHash
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return phc{}, ErrMalformedHash
	}
	var values [3]uint64
	bitSizes := [3]int{32, 32, 8}
	for i, prefix := range [3]string{"m=", "t=", "p="} {
		value, ok := strings.CutPrefix(params[i], prefix)
		if !ok {
			return phc{}, ErrMalformedHash
		}
		n, err := strconv.ParseUint(value, 10, bitSizes[i])
		if err != nil {
			return phc{}, ErrMalformedHash
		}
		values[i] = n
	}
	p := phc{memory: uint32(values[0]), time: uint32(values[1]), threads: uint8(values[2])}
	if p.memory == 0 || p.memory > maxArgonMemoryKiB || p.time == 0 || p.threads == 0 {
		return phc{}, ErrMalformedHash
	}
	var err error
	if p.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(p.salt) == 0 {
		return phc{}, ErrMalformedHash
	}
	if p.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(p.key) == 0 {
		return phc{}, ErrMalformedHash
	}
	return p, nil
}
