// Package postgres implements the application's stores on PostgreSQL.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open connects a pool and verifies connectivity. A malformed url yields ErrInvalidDatabaseURL.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := parseURL(url)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return pool, nil
}
