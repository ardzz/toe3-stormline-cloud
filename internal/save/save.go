// Package save owns an account's cloud-save revisions: optimistic uploads, history, retention and cleanup.
package save

import (
	"context"
	"fmt"
	"time"
)

// pendingTTL bounds an upload: its blob write must finish this long after the reservation. Older pending revisions
// are expired, and a queued blob is kept at least this long because the abandoned upload that owns it may still be
// writing it.
const pendingTTL = time.Hour

// Revision is one stored snapshot. SHA256 is the lowercase hex digest of the stored bytes.
type Revision struct {
	Rev       int64
	CreatedAt time.Time
	Size      int64
	SHA256    string
	Device    string
	Reason    string
	GameBuild int64
	BlobKey   string
}

// Meta is the client-supplied description of an upload.
type Meta struct {
	SHA256    string
	Device    string
	Reason    string
	GameBuild int64
}

// Store keeps revision metadata. Revision numbers are per account, start at 1 and are never reused.
type Store interface {
	// Reserve allocates the next revision as a pending row keyed BlobKey(userID, rev), provided the account's head is
	// still expectedHead (0: no saves yet); otherwise it returns *StaleError.
	Reserve(ctx context.Context, userID, expectedHead int64, r Revision) (Revision, error)
	// Commit makes pending revision rev the head if the head is still expectedHead, then queues the blobs of committed
	// revisions beyond the newest keep for deletion. On *StaleError the pending revision is queued for deletion as
	// well; ErrReservationLost means the pending revision no longer exists and its blob is already queued.
	Commit(ctx context.Context, userID, expectedHead, rev int64, keep int) error
	// Abort queues a pending revision's blob for deletion; unknown revisions are a no-op.
	Abort(ctx context.Context, userID, rev int64) error
	// List returns committed revisions, newest first.
	List(ctx context.Context, userID int64) ([]Revision, error)
	// Head returns the head revision or ErrNoSaves.
	Head(ctx context.Context, userID int64) (Revision, error)
	// ByRev returns a committed revision or ErrNotFound.
	ByRev(ctx context.Context, userID, rev int64) (Revision, error)
}

// Collector is the queue of blob keys that no revision references any more.
type Collector interface {
	// ExpirePending removes pending revisions created before cutoff and queues their blobs.
	ExpirePending(ctx context.Context, cutoff time.Time) (int64, error)
	// GarbageKeys returns up to limit keys queued before queuedBefore, oldest first.
	GarbageKeys(ctx context.Context, queuedBefore time.Time, limit int) ([]string, error)
	// ForgetGarbage removes a key from the queue once its blob is gone.
	ForgetGarbage(ctx context.Context, key string) error
}

// BlobKey is the storage key of an account's revision.
func BlobKey(userID, rev int64) string { return fmt.Sprintf("u/%d/%d.json.gz", userID, rev) }
