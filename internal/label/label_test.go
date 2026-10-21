package label

import (
	"strings"
	"testing"
)

func TestClean(t *testing.T) {
	if got := Clean("  Pixel\x00 7\n ", 64); got != "Pixel 7" {
		t.Errorf("Clean = %q, want %q", got, "Pixel 7")
	}
	if got := Clean(strings.Repeat("é", 70), 64); len([]rune(got)) != 64 {
		t.Errorf("Clean kept %d runes, want 64", len([]rune(got)))
	}
	if got := Clean("\t\n", 64); got != "" {
		t.Errorf("Clean(controls) = %q, want empty", got)
	}
}
