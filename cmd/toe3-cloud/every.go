package main

import (
	"context"
	"time"
)

// every runs fn at once and then at each interval until ctx is done. It never calls fn once ctx is done, and returns
// after fn's last run.
func every(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for ctx.Err() == nil {
		fn(ctx)
		select {
		case <-ctx.Done():
		case <-tick.C:
		}
	}
}
