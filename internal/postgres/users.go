package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

var _ account.UserAdmin = (*Users)(nil)

// Users is the PostgreSQL user store.
type Users struct{ pool *pgxpool.Pool }

// NewUsers returns a Users store on pool.
func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool} }

// Create inserts an account and maps a duplicate username to account.ErrUsernameTaken.
func (u *Users) Create(ctx context.Context, username, pwHash string) (int64, error) {
	var id int64
	err := u.pool.QueryRow(ctx, `INSERT INTO users (username, pw_hash) VALUES ($1, $2) RETURNING id`, username, pwHash).Scan(&id)
	if isUniqueViolation(err) {
		return 0, account.ErrUsernameTaken
	}
	return id, err
}

// ByUsername loads an account or returns account.ErrNotFound.
func (u *Users) ByUsername(ctx context.Context, username string) (account.User, error) {
	var usr account.User
	var disabledAt *time.Time
	err := u.pool.QueryRow(ctx, `SELECT id, username, pw_hash, disabled_at FROM users WHERE username = $1`, username).
		Scan(&usr.ID, &usr.Username, &usr.PwHash, &disabledAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return account.User{}, account.ErrNotFound
	}
	if err != nil {
		return account.User{}, err
	}
	usr.Disabled = disabledAt != nil
	return usr, nil
}

// SetPassword replaces the stored hash and revokes the account's sessions in one transaction.
func (u *Users) SetPassword(ctx context.Context, username, pwHash string) (int64, error) {
	return u.updateAndRevoke(ctx, true, `UPDATE users SET pw_hash = $2 WHERE username = $1 RETURNING id`, username, pwHash)
}

// SetDisabled sets or clears disabled_at, keeping the original disable time on repeats. Disabling revokes the
// account's sessions in the same transaction; enabling leaves them alone.
func (u *Users) SetDisabled(ctx context.Context, username string, disabled bool) (int64, error) {
	return u.updateAndRevoke(ctx, disabled, `UPDATE users SET disabled_at = CASE WHEN $2 THEN COALESCE(disabled_at, now()) ELSE NULL END
		WHERE username = $1 RETURNING id`, username, disabled)
}

// Delete removes an account; sessions cascade.
func (u *Users) Delete(ctx context.Context, username string) (int64, error) {
	return scanID(u.pool.QueryRow(ctx, `DELETE FROM users WHERE username = $1 RETURNING id`, username))
}

// updateAndRevoke runs an UPDATE ... RETURNING id and, if revoke is set, deletes that account's sessions in the same
// transaction. The UPDATE's row lock serializes it with Sessions.Create.
func (u *Users) updateAndRevoke(ctx context.Context, revoke bool, query string, args ...any) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, u.pool, func(tx pgx.Tx) error {
		var err error
		if id, err = scanID(tx.QueryRow(ctx, query, args...)); err != nil || !revoke {
			return err
		}
		_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id)
		return err
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// scanID reads a RETURNING id row, mapping no row to account.ErrNotFound.
func scanID(row pgx.Row) (int64, error) {
	var id int64
	err := row.Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, account.ErrNotFound
	}
	return id, err
}
