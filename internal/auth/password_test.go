package auth

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

var phcRE = regexp.MustCompile(`^\$argon2id\$v=19\$m=19456,t=2,p=1\$[A-Za-z0-9+/]{22}\$[A-Za-z0-9+/]{43}$`)

func TestHashVerify(t *testing.T) {
	h, ctx := NewHasher(2), context.Background()
	enc, err := h.Hash(ctx, "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !phcRE.MatchString(enc) {
		t.Fatalf("unexpected PHC string %q", enc)
	}
	if ok, err := h.Verify(ctx, "correct horse battery", enc); err != nil || !ok {
		t.Fatalf("Verify(correct) = %v, %v", ok, err)
	}
	if ok, err := h.Verify(ctx, "wrong horse battery", enc); err != nil || ok {
		t.Fatalf("Verify(wrong) = %v, %v", ok, err)
	}
}

func TestHashUsesFreshSalt(t *testing.T) {
	h, ctx := NewHasher(2), context.Background()
	a, _ := h.Hash(ctx, "correct horse battery")
	b, _ := h.Hash(ctx, "correct horse battery")
	if a == b {
		t.Fatal("two hashes of the same password are identical")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	h := NewHasher(1)
	for _, enc := range []string{
		"",
		"$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E$a2V5",
		"$argon2id$v=18$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E$a2V5",
		"$argon2id$v=19$m=0,t=2,p=1$c2FsdHNhbHRzYWx0c2E$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$!!!$a2V5",
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E",
	} {
		if _, err := h.Verify(context.Background(), "x", enc); !errors.Is(err, ErrMalformedHash) {
			t.Errorf("Verify(%q) err = %v, want ErrMalformedHash", enc, err)
		}
	}
}

func TestVerifyRejectsMalformedNumericFields(t *testing.T) {
	h, ctx := NewHasher(1), context.Background()
	const password = "correct horse battery"
	encoded, err := h.Hash(ctx, password)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, version, params string
	}{
		{"version trailing junk", "v=19junk", "m=19456,t=2,p=1"},
		{"memory trailing junk", "v=19", "m=19456junk,t=2,p=1"},
		{"time trailing junk", "v=19", "m=19456,t=2junk,p=1"},
		{"threads trailing junk", "v=19", "m=19456,t=2,p=1junk"},
		{"reordered keys", "v=19", "t=2,m=19456,p=1"},
		{"missing memory", "v=19", "t=2,p=1"},
		{"missing time", "v=19", "m=19456,p=1"},
		{"missing threads", "v=19", "m=19456,t=2"},
		{"extra key", "v=19", "m=19456,t=2,p=1,x=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parts := strings.Split(encoded, "$")
			parts[2], parts[3] = tc.version, tc.params
			if _, err := h.Verify(ctx, password, strings.Join(parts, "$")); !errors.Is(err, ErrMalformedHash) {
				t.Fatalf("Verify malformed numeric fields: err = %v, want ErrMalformedHash", err)
			}
		})
	}
}

func TestHasherHonoursContextWhileSaturated(t *testing.T) {
	h := NewHasher(1)
	if err := h.acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer h.release()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "correct horse battery"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Hash while saturated: err = %v, want DeadlineExceeded", err)
	}
}
