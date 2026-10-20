package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

var _ account.Sessions = (*Sessions)(nil)

// Sessions is the PostgreSQL session store.
type Sessions struct{ pool *pgxpool.Pool }

// NewSessions returns a Sessions store on pool.
func NewSessions(pool *pgxpool.Pool) *Sessions { return &Sessions{pool: pool} }

// Create stores sess only if its account is enabled and still has password hash pwHash, and reports whether it
// stored. The user row is share-locked until the insert commits, so Users.SetPassword and SetDisabled either see
// the new session and revoke it, or commit first and make the check fail.
func (s *Sessions) Create(ctx context.Context, sess account.Session, pwHash string) (bool, error) {
	stored := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM users WHERE id = $1 AND pw_hash = $2 AND disabled_at IS NULL FOR SHARE`,
			sess.UserID, pwHash).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, device, created_at, last_used_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, sess.TokenHash, sess.UserID, sess.Device, sess.CreatedAt, sess.LastUsedAt, sess.ExpiresAt); err != nil {
			return err
		}
		stored = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return stored, nil
}

// ByHash loads a session of an enabled account or returns account.ErrNotFound.
func (s *Sessions) ByHash(ctx context.Context, hash []byte) (account.Session, error) {
	var out account.Session
	err := s.pool.QueryRow(ctx, `SELECT s.token_hash, s.user_id, s.device, s.created_at, s.last_used_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND u.disabled_at IS NULL`, hash).
		Scan(&out.TokenHash, &out.UserID, &out.Device, &out.CreatedAt, &out.LastUsedAt, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.Session{}, account.ErrNotFound
	}
	if err != nil {
		return account.Session{}, err
	}
	return out, nil
}

// Touch slides the expiry only if the session was last used at or before staleBefore. The check and the update
// are a single statement, so concurrent callers write at most once.
func (s *Sessions) Touch(ctx context.Context, hash []byte, staleBefore, lastUsed, expires time.Time) (bool, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE sessions SET last_used_at = $3, expires_at = $4
		WHERE token_hash = $1 AND last_used_at <= $2`, hash, staleBefore, lastUsed, expires)
	return tag.RowsAffected() == 1, err
}

// Delete removes one session; unknown hashes are a no-op.
func (s *Sessions) Delete(ctx context.Context, hash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hash)
	return err
}

// DeleteExpired removes sessions whose expiry is at or before now.
func (s *Sessions) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return tag.RowsAffected(), err
}
