package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestEveryRunsAtOnceAndUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan struct{})
	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		every(ctx, time.Hour, func(context.Context) {
			if runs.Add(1) == 1 {
				close(first)
			}
		})
		close(done)
	}()
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("fn did not run before the first tick")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("every did not return after cancellation")
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("runs = %d, want 1", n)
	}
}

func TestEveryRepeatsAtTheInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var runs atomic.Int32
	done := make(chan struct{})
	go func() {
		every(ctx, time.Millisecond, func(context.Context) {
			if runs.Add(1) == 3 {
				cancel()
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("every did not return after cancellation")
	}
	if n := runs.Load(); n != 3 {
		t.Fatalf("runs = %d, want 3", n)
	}
}

func TestEveryNeverRunsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ran := false
	every(ctx, time.Millisecond, func(context.Context) { ran = true })
	if ran {
		t.Fatal("fn ran with an already-cancelled context")
	}
}
