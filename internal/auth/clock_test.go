package auth

import "time"

type fakeClock struct{ t time.Time }

func newFakeClock() *fakeClock               { return &fakeClock{t: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)} }
func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }
