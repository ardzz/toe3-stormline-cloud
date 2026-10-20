package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestRateLimiterBurstThenRefill(t *testing.T) {
	c := newFakeClock()
	r := NewRateLimiter(c.Now, 20, time.Minute)
	for i := range 20 {
		if d := r.Allow("10.0.0.1"); d != 0 {
			t.Fatalf("request %d limited (%v)", i+1, d)
		}
	}
	d := r.Allow("10.0.0.1")
	if d <= 0 || d > 3*time.Second {
		t.Fatalf("21st request wait = %v, want (0, 3s]", d)
	}
	c.Advance(d)
	if d := r.Allow("10.0.0.1"); d != 0 {
		t.Fatalf("after waiting, Allow = %v, want 0", d)
	}
	if d := r.Allow("10.0.0.2"); d != 0 {
		t.Fatalf("other key limited: %v", d)
	}
}

func TestRateLimiterPrunesIdleKeys(t *testing.T) {
	c := newFakeClock()
	r := NewRateLimiter(c.Now, 20, time.Minute)
	for i := range pruneThreshold {
		r.Allow(fmt.Sprintf("10.0.%d.%d", i/256, i%256))
	}
	c.Advance(time.Minute)
	r.Allow("192.0.2.1")
	if n := len(r.buckets); n != 1 {
		t.Fatalf("%d buckets after prune, want 1", n)
	}
}
