package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestUserCommandErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"user", "add"}, "usage: toe3-cloud user"},
		{[]string{"user", "frob", "alice"}, `unknown user command "frob"`},
		{[]string{"user", "disable", "alice"}, "TOE3_DATABASE_URL is required"},
		{[]string{"migrate"}, "TOE3_DATABASE_URL is required"},
	}
	for _, c := range cases {
		var out, errb bytes.Buffer
		if code := run(context.Background(), c.args, noEnv, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), c.want) {
			t.Errorf("run(%q): exit %d, stderr %q; want 1 containing %q", c.args, code, errb.String(), c.want)
		}
	}
}

func TestMigrateHidesMalformedURLCredentials(t *testing.T) {
	getenv := func(k string) string {
		if k == "TOE3_DATABASE_URL" {
			return "postgres://operator:supersecret"
		}
		return ""
	}
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"migrate"}, getenv, strings.NewReader(""), &out, &errb); code != 1 ||
		strings.Contains(errb.String(), "supersecret") {
		t.Fatalf("run(migrate): exit %d, stderr %q; want 1 without the password", code, errb.String())
	}
}
