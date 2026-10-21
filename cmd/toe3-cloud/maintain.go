package main

import (
	"context"
	"log/slog"
)

type blobSweeper interface {
	Sweep(ctx context.Context) (int, error)
}

type sessionPurger interface {
	PurgeExpiredSessions(ctx context.Context) (int64, error)
}

// maintenance returns the periodic cleanup: unreferenced blobs, then expired sessions. Failures are logged and
// retried at the next run; failures caused by shutdown are not logged.
func maintenance(log *slog.Logger, blobs blobSweeper, sessions sessionPurger) func(context.Context) {
	return func(ctx context.Context) {
		if n, err := blobs.Sweep(ctx); err != nil {
			if ctx.Err() == nil {
				log.Error("blob sweep failed", "deleted", n, "error", err)
			}
		} else if n > 0 {
			log.Info("deleted unreferenced blobs", "count", n)
		}
		if n, err := sessions.PurgeExpiredSessions(ctx); err != nil {
			if ctx.Err() == nil {
				log.Error("session purge failed", "error", err)
			}
		} else if n > 0 {
			log.Info("deleted expired sessions", "count", n)
		}
	}
}
