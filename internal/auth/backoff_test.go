package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestBackoffLocksFromFifthFailureAndDoubles(t *testing.T) {
	c := newFakeClock()
	b := NewBackoff(c.Now, 5, time.Second, 15*time.Minute)
	for i := 1; i <= 4; i++ {
		b.Fail("alice")
		if d := b.Allow("alice"); d != 0 {
			t.Fatalf("after %d failures Allow = %v, want 0", i, d)
		}
	}
	want := time.Second
	for i := 5; i <= 9; i++ {
		b.Fail("alice")
		if d := b.Allow("alice"); d != want {
			t.Fatalf("after %d failures Allow = %v, want %v", i, d, want)
		}
		c.Advance(want)
		if d := b.Allow("alice"); d != 0 {
			t.Fatalf("lock after %d failures did not expire: %v", i, d)
		}
		want *= 2
	}
}

func TestBackoffCapsAtMax(t *testing.T) {
	c := newFakeClock()
	b := NewBackoff(c.Now, 5, time.Second, 15*time.Minute)
	for i := 0; i < 30; i++ {
		b.Fail("alice")
	}
	if d := b.Allow("alice"); d != 15*time.Minute {
		t.Fatalf("Allow = %v, want 15m", d)
	}
}

func TestBackoffSuccessResetsAndKeysAreIndependent(t *testing.T) {
	c := newFakeClock()
	b := NewBackoff(c.Now, 5, time.Second, 15*time.Minute)
	for i := 0; i < 5; i++ {
		b.Fail("alice")
	}
	if d := b.Allow("bob"); d != 0 {
		t.Fatalf("bob locked by alice's failures: %v", d)
	}
	c.Advance(time.Second)
	b.Succeed("alice")
	for i := 0; i < 4; i++ {
		b.Fail("alice")
	}
	if d := b.Allow("alice"); d != 0 {
		t.Fatalf("Allow after reset + 4 failures = %v, want 0", d)
	}
}

func TestBackoffPrunesStaleKeys(t *testing.T) {
	c := newFakeClock()
	b := NewBackoff(c.Now, 5, time.Second, time.Minute)
	for i := 0; i < pruneThreshold; i++ {
		b.Fail(fmt.Sprintf("user%d", i))
	}
	c.Advance(time.Minute)
	b.Fail("fresh")
	if n := len(b.state); n != 1 {
		t.Fatalf("%d keys after prune, want 1", n)
	}
}
