package save

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

// scriptedCollector is an in-memory garbage queue recording the sweeper's calls.
type scriptedCollector struct {
	queue   []string
	cutoffs []time.Time
	limits  []int
}

func (c *scriptedCollector) ExpirePending(_ context.Context, cutoff time.Time) (int64, error) {
	c.cutoffs = append(c.cutoffs, cutoff)
	return 0, nil
}

func (c *scriptedCollector) GarbageKeys(_ context.Context, limit int) ([]string, error) {
	c.limits = append(c.limits, limit)
	return slices.Clone(c.queue[:min(limit, len(c.queue))]), nil
}

func (c *scriptedCollector) ForgetGarbage(_ context.Context, key string) error {
	c.queue = slices.DeleteFunc(c.queue, func(k string) bool { return k == key })
	return nil
}

// failingDelete fails Delete for one key.
type failingDelete struct {
	*memBlobs
	fail string
}

func (f failingDelete) Delete(ctx context.Context, key string) error {
	if key == f.fail {
		return errors.New("bucket unavailable")
	}
	return f.memBlobs.Delete(ctx, key)
}

func TestSweepExpiresPendingAndDrainsQueueInBatches(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	gc, blobs := &scriptedCollector{}, newMemBlobs()
	for i := range 250 {
		k := fmt.Sprintf("u/1/%d.json.gz", i+1)
		gc.queue = append(gc.queue, k)
		blobs.data[k] = []byte("x")
	}
	n, err := NewSweeper(gc, blobs, func() time.Time { return now }).Sweep(context.Background())
	if err != nil || n != 250 {
		t.Fatalf("Sweep = %d, %v; want 250, nil", n, err)
	}
	if len(gc.cutoffs) != 1 || !gc.cutoffs[0].Equal(now.Add(-time.Hour)) {
		t.Fatalf("cutoffs = %v, want [%v]", gc.cutoffs, now.Add(-time.Hour))
	}
	if !slices.Equal(gc.limits, []int{100, 100, 100}) || len(gc.queue) != 0 || len(blobs.data) != 0 {
		t.Fatalf("limits %v, queue %d, blobs %d", gc.limits, len(gc.queue), len(blobs.data))
	}
}

func TestSweepKeepsKeyQueuedWhenDeleteFails(t *testing.T) {
	gc := &scriptedCollector{queue: []string{"u/1/1.json.gz", "u/1/2.json.gz"}}
	n, err := NewSweeper(gc, failingDelete{memBlobs: newMemBlobs(), fail: "u/1/2.json.gz"}, nil).Sweep(context.Background())
	if err == nil || n != 1 || !slices.Equal(gc.queue, []string{"u/1/2.json.gz"}) {
		t.Fatalf("Sweep = %d, %v, queue %v; want 1, error, [u/1/2.json.gz]", n, err, gc.queue)
	}
}
