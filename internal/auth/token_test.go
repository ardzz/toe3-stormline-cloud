package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestNewToken(t *testing.T) {
	tok, hash, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		t.Fatalf("token %q decodes to %d bytes (%v), want 32", tok, len(raw), err)
	}
	if len(hash) != sha256.Size || !bytes.Equal(hash, HashToken(tok)) {
		t.Fatal("returned hash differs from HashToken(token)")
	}
	if tok2, _, _ := NewToken(); tok2 == tok {
		t.Fatal("two tokens are identical")
	}
}
