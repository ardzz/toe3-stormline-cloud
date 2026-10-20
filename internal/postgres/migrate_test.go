//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

func TestMigrateIsIdempotent(t *testing.T) {
	url := pgtest.URL(t)
	if err := postgres.Migrate(context.Background(), url); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

func TestMigrateRejectsNewerSchema(t *testing.T) {
	url := pgtest.URL(t)
	pool, err := postgres.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(context.Background(), `INSERT INTO goose_db_version (version_id, is_applied) VALUES (9999, true)`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.Migrate(context.Background(), url); !errors.Is(err, postgres.ErrSchemaTooNew) {
		t.Fatalf("Migrate = %v, want ErrSchemaTooNew", err)
	}
}
