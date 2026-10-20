package auth

import (
	"sync"
	"time"
)

// pruneThreshold is the key count at which the throttles drop entries that no longer matter.
const pruneThreshold = 4096

// Backoff locks out a key (a username) after repeated failures: the free-th consecutive failure locks it for
// base, each further failure doubles the lock up to max, and a success clears it.
type Backoff struct {
	mu    sync.Mutex
	now   func() time.Time
	free  int
	base  time.Duration
	max   time.Duration
	state map[string]*backoffState
}

type backoffState struct {
	failures int
	until    time.Time
	last     time.Time
}

// NewBackoff returns a Backoff reading time from now.
func NewBackoff(now func() time.Time, free int, base, max time.Duration) *Backoff {
	return &Backoff{now: now, free: free, base: base, max: max, state: map[string]*backoffState{}}
}

// Allow returns 0 if key may attempt now, otherwise the remaining lockout.
func (b *Backoff) Allow(key string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.state[key]
	if !ok {
		return 0
	}
	if d := s.until.Sub(b.now()); d > 0 {
		return d
	}
	return 0
}

// Fail records one failed attempt for key.
func (b *Backoff) Fail(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	s, ok := b.state[key]
	if !ok {
		if len(b.state) >= pruneThreshold {
			b.prune(now)
		}
		s = &backoffState{}
		b.state[key] = s
	}
	s.failures++
	s.last = now
	if s.failures >= b.free {
		s.until = now.Add(b.lockout(s.failures - b.free))
	}
}

// Succeed clears key.
func (b *Backoff) Succeed(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, key)
}

func (b *Backoff) lockout(step int) time.Duration {
	d := b.base
	for i := 0; i < step && d < b.max; i++ {
		d *= 2
	}
	if d > b.max {
		return b.max
	}
	return d
}

// prune drops keys whose lock has expired and whose last failure is older than max.
func (b *Backoff) prune(now time.Time) {
	for k, s := range b.state {
		if now.Sub(s.last) >= b.max && !now.Before(s.until) {
			delete(b.state, k)
		}
	}
}
