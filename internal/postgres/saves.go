package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

var (
	_ save.Store     = (*Saves)(nil)
	_ save.Collector = (*Saves)(nil)
)

// Saves is the PostgreSQL save-revision store and blob-garbage queue. Locks are taken in the order user row,
// revision rows (ascending rev), garbage keys; expiry skips revision rows that are already locked.
type Saves struct{ pool *pgxpool.Pool }

// NewSaves returns a Saves store on pool.
func NewSaves(pool *pgxpool.Pool) *Saves { return &Saves{pool: pool} }

const revisionColumns = `rev, created_at, size, sha256, device, reason, game_build, blob_key`

// queuePending removes one pending revision and queues its blob, atomically.
const queuePending = `WITH gone AS (
	DELETE FROM save_revisions WHERE user_id = $1 AND rev = $2 AND state = 'pending' RETURNING blob_key)
	INSERT INTO blob_garbage (blob_key) SELECT blob_key FROM gone ON CONFLICT DO NOTHING`

// pruneCommitted removes committed revisions beyond the newest $2 and queues their blobs.
const pruneCommitted = `WITH old AS (
	DELETE FROM save_revisions WHERE user_id = $1 AND state = 'committed' AND rev NOT IN (
		SELECT rev FROM save_revisions WHERE user_id = $1 AND state = 'committed' ORDER BY rev DESC LIMIT $2)
	RETURNING blob_key)
	INSERT INTO blob_garbage (blob_key) SELECT blob_key FROM old ON CONFLICT DO NOTHING`

func scanRevision(row pgx.Row) (save.Revision, error) {
	var r save.Revision
	err := row.Scan(&r.Rev, &r.CreatedAt, &r.Size, &r.SHA256, &r.Device, &r.Reason, &r.GameBuild, &r.BlobKey)
	return r, err
}

// Reserve allocates the next revision number as a pending row if the head is still expectedHead.
func (s *Saves) Reserve(ctx context.Context, userID, expectedHead int64, r save.Revision) (save.Revision, error) {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var head int64
		if err := tx.QueryRow(ctx, `SELECT head_rev, next_rev FROM users WHERE id = $1 FOR UPDATE`, userID).
			Scan(&head, &r.Rev); err != nil {
			return err
		}
		if head != expectedHead {
			return &save.StaleError{Latest: head}
		}
		r.BlobKey = save.BlobKey(userID, r.Rev)
		if _, err := tx.Exec(ctx, `UPDATE users SET next_rev = next_rev + 1 WHERE id = $1`, userID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO save_revisions
			(user_id, rev, state, blob_key, size, sha256, device, reason, game_build, created_at)
			VALUES ($1, $2, 'pending', $3, $4, $5, $6, $7, $8, $9)`,
			userID, r.Rev, r.BlobKey, r.Size, r.SHA256, r.Device, r.Reason, r.GameBuild, r.CreatedAt)
		return err
	})
	if err != nil {
		return save.Revision{}, err
	}
	return r, nil
}

// Commit makes pending revision rev the head if the head is still expectedHead and applies retention.
func (s *Saves) Commit(ctx context.Context, userID, expectedHead, rev int64, keep int) error {
	var stale *save.StaleError
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var head int64
		err := tx.QueryRow(ctx, `SELECT head_rev FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&head)
		if errors.Is(err, pgx.ErrNoRows) {
			return save.ErrReservationLost
		}
		if err != nil {
			return err
		}
		var one int
		err = tx.QueryRow(ctx, `SELECT 1 FROM save_revisions WHERE user_id = $1 AND rev = $2 AND state = 'pending' FOR UPDATE`,
			userID, rev).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return save.ErrReservationLost
		}
		if err != nil {
			return err
		}
		if head != expectedHead {
			stale = &save.StaleError{Latest: head}
			_, err := tx.Exec(ctx, queuePending, userID, rev)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE save_revisions SET state = 'committed' WHERE user_id = $1 AND rev = $2`, userID, rev); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET head_rev = $2 WHERE id = $1`, userID, rev); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, pruneCommitted, userID, keep)
		return err
	})
	if err != nil {
		return err
	}
	if stale != nil {
		return stale
	}
	return nil
}

// abortUpload drops a pending revision if it still exists and queues its blob key unless a committed revision owns it.
const abortUpload = `WITH gone AS (
	DELETE FROM save_revisions WHERE user_id = $1 AND rev = $2 AND state = 'pending')
	INSERT INTO blob_garbage (blob_key) SELECT $3 WHERE NOT EXISTS (
		SELECT 1 FROM save_revisions WHERE blob_key = $3 AND state = 'committed')
	ON CONFLICT DO NOTHING`

// Abort ends an upload that will not commit. The key is queued even when the pending row is gone, because the
// reservation may have been expired or its account deleted, and the key swept, while the blob was still being written.
func (s *Saves) Abort(ctx context.Context, userID, rev int64) error {
	_, err := s.pool.Exec(ctx, abortUpload, userID, rev, save.BlobKey(userID, rev))
	return err
}

// List returns committed revisions, newest first.
func (s *Saves) List(ctx context.Context, userID int64) ([]save.Revision, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+revisionColumns+` FROM save_revisions
		WHERE user_id = $1 AND state = 'committed' ORDER BY rev DESC`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (save.Revision, error) { return scanRevision(row) })
}

// Head returns the head revision or save.ErrNoSaves.
func (s *Saves) Head(ctx context.Context, userID int64) (save.Revision, error) {
	r, err := scanRevision(s.pool.QueryRow(ctx, `SELECT `+revisionColumns+` FROM save_revisions
		WHERE user_id = $1 AND state = 'committed' AND rev = (SELECT head_rev FROM users WHERE id = $1)`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return save.Revision{}, save.ErrNoSaves
	}
	return r, err
}

// ByRev returns a committed revision or save.ErrNotFound.
func (s *Saves) ByRev(ctx context.Context, userID, rev int64) (save.Revision, error) {
	r, err := scanRevision(s.pool.QueryRow(ctx, `SELECT `+revisionColumns+` FROM save_revisions
		WHERE user_id = $1 AND rev = $2 AND state = 'committed'`, userID, rev))
	if errors.Is(err, pgx.ErrNoRows) {
		return save.Revision{}, save.ErrNotFound
	}
	return r, err
}

// ExpirePending removes pending revisions created before cutoff, queues their blobs and returns how many it removed.
// Revisions locked by an in-flight upload are left for a later sweep.
func (s *Saves) ExpirePending(ctx context.Context, cutoff time.Time) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `WITH due AS (
		SELECT user_id, rev FROM save_revisions
		WHERE state = 'pending' AND created_at < $1
		ORDER BY user_id, rev
		FOR UPDATE SKIP LOCKED),
	gone AS (
		DELETE FROM save_revisions s USING due
		WHERE s.user_id = due.user_id AND s.rev = due.rev
		RETURNING s.blob_key),
	queued AS (
		INSERT INTO blob_garbage (blob_key) SELECT blob_key FROM gone ON CONFLICT DO NOTHING)
	SELECT count(*) FROM gone`, cutoff).Scan(&n)
	return n, err
}

// GarbageKeys returns up to limit keys queued before queuedBefore, oldest first.
func (s *Saves) GarbageKeys(ctx context.Context, queuedBefore time.Time, limit int) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT blob_key FROM blob_garbage WHERE enqueued_at < $1 ORDER BY enqueued_at, blob_key LIMIT $2`, queuedBefore, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// ForgetGarbage removes a key from the queue.
func (s *Saves) ForgetGarbage(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM blob_garbage WHERE blob_key = $1`, key)
	return err
}
