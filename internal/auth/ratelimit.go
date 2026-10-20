package auth

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter is a per-key token bucket (a client IP): burst requests, refilled evenly over per.
type RateLimiter struct {
	mu      sync.Mutex
	now     func() time.Time
	every   rate.Limit
	burst   int
	idle    time.Duration
	buckets map[string]*limiterEntry
}

type limiterEntry struct {
	lim  *rate.Limiter
	seen time.Time
}

// NewRateLimiter returns a limiter reading time from now; burst must be at least 1.
func NewRateLimiter(now func() time.Time, burst int, per time.Duration) *RateLimiter {
	return &RateLimiter{now: now, every: rate.Every(per / time.Duration(burst)), burst: burst, idle: per,
		buckets: map[string]*limiterEntry{}}
}

// Allow consumes one token for key and returns 0, or returns the wait until a token is available.
func (r *RateLimiter) Allow(key string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	e, ok := r.buckets[key]
	if !ok {
		if len(r.buckets) >= pruneThreshold {
			r.prune(now)
		}
		e = &limiterEntry{lim: rate.NewLimiter(r.every, r.burst)}
		r.buckets[key] = e
	}
	e.seen = now
	res := e.lim.ReserveN(now, 1)
	if !res.OK() {
		return r.idle
	}
	if d := res.DelayFrom(now); d > 0 {
		res.CancelAt(now)
		return d
	}
	return 0
}

// prune drops buckets idle for a full refill period; they would be full again anyway.
func (r *RateLimiter) prune(now time.Time) {
	for k, e := range r.buckets {
		if now.Sub(e.seen) >= r.idle {
			delete(r.buckets, k)
		}
	}
}
