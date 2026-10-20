package save

import (
	"context"
	"errors"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

// Options configures a Service.
type Options struct {
	MaxBytes  int64            // largest accepted snapshot
	Retention int              // committed revisions kept per account
	Now       func() time.Time // defaults to time.Now
}

// Service implements the player-facing save flows.
type Service struct {
	store Store
	blobs blob.Store
	opts  Options
}

// NewService wires a Service.
func NewService(store Store, blobs blob.Store, opts Options) *Service {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{store: store, blobs: blobs, opts: opts}
}

// Upload stores data as the account's new head if the head is still expectedHead (0: no saves yet).
func (s *Service) Upload(ctx context.Context, userID, expectedHead int64, data []byte, m Meta) (Revision, error) {
	if expectedHead < 0 {
		return Revision{}, ErrInvalidMeta
	}
	r, err := describe(data, m, s.opts.MaxBytes)
	if err != nil {
		return Revision{}, err
	}
	r.CreatedAt = s.opts.Now().UTC()
	if r, err = s.store.Reserve(ctx, userID, expectedHead, r); err != nil {
		return Revision{}, err
	}
	// A queued blob is kept for pendingTTL in case its upload is still writing, so the write must not outlive that.
	putCtx, cancel := context.WithDeadline(ctx, r.CreatedAt.Add(pendingTTL))
	err = s.blobs.Put(putCtx, r.BlobKey, data)
	cancel()
	if err != nil {
		return Revision{}, errors.Join(err, s.store.Abort(context.WithoutCancel(ctx), userID, r.Rev))
	}
	// Every Commit failure leaves the blob to the garbage queue: a stale or lost reservation has already queued it,
	// and a pending row that survives a transient failure is expired into the queue.
	if err := s.store.Commit(ctx, userID, expectedHead, r.Rev, s.opts.Retention); err != nil {
		return Revision{}, err
	}
	return r, nil
}

// List returns the account's committed revisions, newest first.
func (s *Service) List(ctx context.Context, userID int64) ([]Revision, error) {
	return s.store.List(ctx, userID)
}

// Latest returns the head revision and its snapshot.
func (s *Service) Latest(ctx context.Context, userID int64) (Revision, []byte, error) {
	r, err := s.store.Head(ctx, userID)
	if err != nil {
		return Revision{}, nil, err
	}
	return s.load(ctx, r)
}

// Get returns a committed revision and its snapshot.
func (s *Service) Get(ctx context.Context, userID, rev int64) (Revision, []byte, error) {
	r, err := s.store.ByRev(ctx, userID, rev)
	if err != nil {
		return Revision{}, nil, err
	}
	return s.load(ctx, r)
}

func (s *Service) load(ctx context.Context, r Revision) (Revision, []byte, error) {
	data, err := s.blobs.Get(ctx, r.BlobKey)
	if errors.Is(err, blob.ErrNotFound) {
		return Revision{}, nil, ErrBlobMissing
	}
	if err != nil {
		return Revision{}, nil, err
	}
	return r, data, nil
}
