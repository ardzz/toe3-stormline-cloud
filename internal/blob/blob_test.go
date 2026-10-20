package blob

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckKey(t *testing.T) {
	for _, ok := range []string{"u/1/2.json.gz", "a", "a-b_c.d/e"} {
		if err := CheckKey(ok); err != nil {
			t.Errorf("CheckKey(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/a", "a/", "a//b", "../a", "a/../b", "a/./b", "A", "a b", `a\b`, strings.Repeat("a", 513)} {
		if err := CheckKey(bad); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("CheckKey(%q) = %v, want ErrInvalidKey", bad, err)
		}
	}
}
