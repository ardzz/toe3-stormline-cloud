package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestRunVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"version"}, noEnv, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	if got := strings.TrimSpace(out.String()); got != "dev" {
		t.Fatalf("version = %q, want dev", got)
	}
}

func TestRunRejectsUnknownAndMissingCommands(t *testing.T) {
	for _, args := range [][]string{{}, {"frobnicate"}} {
		var out, errb bytes.Buffer
		if code := run(context.Background(), args, noEnv, strings.NewReader(""), &out, &errb); code != 2 {
			t.Errorf("run(%q) exit %d, want 2", args, code)
		}
		if !strings.Contains(errb.String(), "usage: toe3-cloud") {
			t.Errorf("run(%q) stderr %q lacks usage", args, errb.String())
		}
	}
}

func TestRunServeReportsConfigErrorsBeforeListening(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run(context.Background(), []string{"serve"}, noEnv, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "TOE3_DATABASE_URL is required") {
		t.Fatalf("stderr %q lacks the configuration error", errb.String())
	}
}
