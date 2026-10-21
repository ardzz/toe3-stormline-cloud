package main

import (
	"context"
	"time"
)

// every runs fn at once and then at each interval until ctx is done; it returns after fn's last run.
func every(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		fn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if ctx.Err() != nil {
				return
			}
		}
	}
}
