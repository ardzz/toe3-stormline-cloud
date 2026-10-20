//go:build integration

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

func TestUserCommandsAgainstPostgres(t *testing.T) {
	url := pgtest.URL(t)
	getenv := func(k string) string {
		if k == "TOE3_DATABASE_URL" {
			return url
		}
		return ""
	}
	steps := []struct {
		stdin    string
		args     []string
		code     int
		contains string
	}{
		{"correct horse battery\n", []string{"user", "add", "Alice"}, 0, "created user"},
		{"correct horse battery\n", []string{"user", "add", "alice"}, 1, "username taken"},
		{"", []string{"user", "disable", "alice"}, 0, "disabled"},
		{"", []string{"user", "enable", "alice"}, 0, "enabled"},
		{"another long secret\n", []string{"user", "passwd", "alice"}, 0, "password updated"},
		{"", []string{"user", "delete", "ghost"}, 1, "not found"},
		{"", []string{"user", "delete", "alice"}, 0, "deleted"},
		{"", []string{"migrate"}, 0, ""},
	}
	for _, s := range steps {
		var out, errb bytes.Buffer
		code := run(context.Background(), s.args, getenv, strings.NewReader(s.stdin), &out, &errb)
		if all := out.String() + errb.String(); code != s.code || !strings.Contains(all, s.contains) {
			t.Fatalf("%q: exit %d, output %q; want %d containing %q", s.args, code, all, s.code, s.contains)
		}
	}
}
