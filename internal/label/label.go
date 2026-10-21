// Package label cleans short client-supplied display labels such as device names.
package label

import (
	"strings"
	"unicode"
)

// Clean drops control characters, trims surrounding space and keeps at most max characters.
func Clean(s string, max int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > max {
		s = string(r[:max])
	}
	return s
}
