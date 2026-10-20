package account

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeUsername(t *testing.T) {
	ok := map[string]string{"Player_1": "player_1", " trim.me ": "trim.me", "a-b": "a-b", strings.Repeat("x", 32): strings.Repeat("x", 32)}
	for in, want := range ok {
		if got, err := NormalizeUsername(in); err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"ab", strings.Repeat("x", 33), "bad name", "üser", "semi;colon", ""} {
		if _, err := NormalizeUsername(in); !errors.Is(err, ErrInvalidUsername) {
			t.Errorf("NormalizeUsername(%q) err = %v, want ErrInvalidUsername", in, err)
		}
	}
}

func TestValidatePasswordCountsRunes(t *testing.T) {
	for _, pw := range []string{strings.Repeat("a", 10), strings.Repeat("a", 256), strings.Repeat("é", 10)} {
		if err := ValidatePassword(pw); err != nil {
			t.Errorf("ValidatePassword(%d runes) = %v", len([]rune(pw)), err)
		}
	}
	for _, pw := range []string{strings.Repeat("a", 9), strings.Repeat("a", 257), strings.Repeat("é", 9)} {
		if err := ValidatePassword(pw); !errors.Is(err, ErrInvalidPassword) {
			t.Errorf("ValidatePassword(%d runes) = %v, want ErrInvalidPassword", len([]rune(pw)), err)
		}
	}
}
