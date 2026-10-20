package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrInvalidDatabaseURL replaces every connection-string parse error, because pgx redacts credentials only on a
// best-effort basis and a malformed URL can echo its password verbatim.
var ErrInvalidDatabaseURL = errors.New("postgres: TOE3_DATABASE_URL is not a valid PostgreSQL connection string")

// parseURL parses a connection string, never retaining the original error.
func parseURL(url string) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, ErrInvalidDatabaseURL
	}
	return cfg, nil
}
