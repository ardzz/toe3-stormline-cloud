package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadLine(t *testing.T) {
	r := strings.NewReader("first\r\nsecond")
	if got, err := readLine(r); err != nil || got != "first" {
		t.Fatalf("first line = %q, %v", got, err)
	}
	if got, err := readLine(r); err != nil || got != "second" {
		t.Fatalf("second line = %q, %v", got, err)
	}
	if _, err := readLine(r); !errors.Is(err, io.EOF) {
		t.Fatalf("at end err = %v, want io.EOF", err)
	}
}

func TestReadCancellableReturnsReadResult(t *testing.T) {
	boom := errors.New("boom")
	got, err := readCancellable(context.Background(), func() (string, error) { return "secret", boom })
	if got != "secret" || err != boom {
		t.Fatalf("readCancellable = %q, %v; want secret, boom", got, err)
	}
}

func TestReadCancellableAbandonsBlockedRead(t *testing.T) {
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := readCancellable(ctx, func() (string, error) { <-block; return "late", nil })
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("readCancellable = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("readCancellable did not return after cancellation")
	}
}
