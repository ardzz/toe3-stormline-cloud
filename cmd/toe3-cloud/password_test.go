package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadPasswordFromPipe(t *testing.T) {
	ctx := context.Background()
	for in, want := range map[string]string{
		"correct horse battery\n": "correct horse battery",
		"a b c\r\nrest":           "a b c",
		"no-newline":              "no-newline",
	} {
		if got, err := readPassword(ctx, strings.NewReader(in), io.Discard); err != nil || got != want {
			t.Errorf("readPassword(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := readPassword(ctx, strings.NewReader("\n"), io.Discard); err == nil {
		t.Error("empty password accepted")
	}
}

func TestReadPasswordFromSilentPipeHonoursCancellation(t *testing.T) {
	r, w := io.Pipe()
	t.Cleanup(func() { w.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readPassword(ctx, r, io.Discard); !errors.Is(err, context.Canceled) {
		t.Fatalf("readPassword = %v, want context.Canceled", err)
	}
}
