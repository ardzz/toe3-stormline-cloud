//go:build integration

// Package pgtest starts a throwaway, migrated PostgreSQL 18 container for integration tests.
package pgtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
)

// URL starts postgres:18, applies migrations and returns its URL; the container stops at test cleanup.
func URL(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	c, err := tcpostgres.Run(ctx, "postgres:18",
		tcpostgres.WithDatabase("toe3"), tcpostgres.WithUsername("toe3"), tcpostgres.WithPassword("toe3-test"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(c); err != nil {
			t.Errorf("stop postgres: %v", err)
		}
	})
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres url: %v", err)
	}
	if err := postgres.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return url
}

// Pool returns a pool on a fresh migrated database, closed at test cleanup.
func Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, err := postgres.Open(context.Background(), URL(t))
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
