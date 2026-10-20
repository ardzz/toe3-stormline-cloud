package save

import (
	"context"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

const (
	pendingTTL = time.Hour // uploads still pending after this are abandoned
	sweepBatch = 100
)

// Sweeper deletes blobs that no revision references: abandoned uploads and pruned or deleted revisions.
type Sweeper struct {
	gc    Collector
	blobs blob.Store
	now   func() time.Time
}

// NewSweeper wires a Sweeper; now defaults to time.Now.
func NewSweeper(gc Collector, blobs blob.Store, now func() time.Time) *Sweeper {
	if now == nil {
		now = time.Now
	}
	return &Sweeper{gc: gc, blobs: blobs, now: now}
}

// Sweep expires uploads pending for more than an hour, then deletes every queued blob. It returns the number of blobs
// deleted; on error the remaining keys stay queued for the next sweep.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	if _, err := s.gc.ExpirePending(ctx, s.now().Add(-pendingTTL)); err != nil {
		return 0, err
	}
	deleted := 0
	for {
		keys, err := s.gc.GarbageKeys(ctx, sweepBatch)
		if err != nil {
			return deleted, err
		}
		for _, k := range keys {
			if err := s.blobs.Delete(ctx, k); err != nil {
				return deleted, err
			}
			if err := s.gc.ForgetGarbage(ctx, k); err != nil {
				return deleted, err
			}
			deleted++
		}
		if len(keys) < sweepBatch {
			return deleted, nil
		}
	}
}
