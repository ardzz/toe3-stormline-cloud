package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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

func TestReadCancellableReturnsReadResult(t *testing.T) {
	boom := errors.New("boom")
	got, err := readCancellable(context.Background(), func() (string, error) { return "secret", boom }, func() {
		t.Error("onCancel called without cancellation")
	})
	if got != "secret" || err != boom {
		t.Fatalf("readCancellable = %q, %v; want secret, boom", got, err)
	}
}

func TestReadCancellableAbandonsBlockedRead(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	ctx, cancel := context.WithCancel(context.Background())
	var cancels atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := readCancellable(ctx, func() (string, error) { <-block; return "late", nil }, func() { cancels.Add(1) })
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || cancels.Load() != 1 {
			t.Fatalf("readCancellable = %v with %d onCancel calls; want context.Canceled and 1", err, cancels.Load())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readCancellable did not return after cancellation")
	}
}
