package save

import (
	"context"
	"sync"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

// scriptedStore returns preset results and records the service's calls. The PostgreSQL store's semantics are
// covered by its integration tests.
type scriptedStore struct {
	reserveErr        error
	commitErr         error
	abortErr          error
	head, byRev       Revision
	headErr, byRevErr error
	list              []Revision

	reserved     []Revision
	commits      [][4]int64 // userID, expectedHead, rev, keep
	aborts       [][2]int64 // userID, rev
	abortCtxErrs []error
}

func (s *scriptedStore) Reserve(_ context.Context, userID, expectedHead int64, r Revision) (Revision, error) {
	s.reserved = append(s.reserved, r)
	if s.reserveErr != nil {
		return Revision{}, s.reserveErr
	}
	r.Rev = expectedHead + 1
	r.BlobKey = BlobKey(userID, r.Rev)
	return r, nil
}

func (s *scriptedStore) Commit(_ context.Context, userID, expectedHead, rev int64, keep int) error {
	s.commits = append(s.commits, [4]int64{userID, expectedHead, rev, int64(keep)})
	return s.commitErr
}

func (s *scriptedStore) Abort(ctx context.Context, userID, rev int64) error {
	s.aborts = append(s.aborts, [2]int64{userID, rev})
	s.abortCtxErrs = append(s.abortCtxErrs, ctx.Err())
	return s.abortErr
}

func (s *scriptedStore) List(context.Context, int64) ([]Revision, error) { return s.list, nil }
func (s *scriptedStore) Head(context.Context, int64) (Revision, error)   { return s.head, s.headErr }
func (s *scriptedStore) ByRev(context.Context, int64, int64) (Revision, error) {
	return s.byRev, s.byRevErr
}

// memBlobs is an in-memory blob.Store with an injectable Put failure.
type memBlobs struct {
	mu           sync.Mutex
	data         map[string][]byte
	putErr       error
	getErr       error
	putDeadlines []time.Time
	deleted      []string
}

func newMemBlobs() *memBlobs { return &memBlobs{data: map[string][]byte{}} }

func (m *memBlobs) Put(ctx context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if deadline, ok := ctx.Deadline(); ok {
		m.putDeadlines = append(m.putDeadlines, deadline)
	}
	if m.putErr != nil {
		return m.putErr
	}
	m.data[key] = append([]byte(nil), data...)
	return nil
}

func (m *memBlobs) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	d, ok := m.data[key]
	if !ok {
		return nil, blob.ErrNotFound
	}
	return d, nil
}

func (m *memBlobs) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, key)
	delete(m.data, key)
	return nil
}
