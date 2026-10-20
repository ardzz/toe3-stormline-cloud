package main

import (
	"context"

	"github.com/ardzz/toe3-stormline-cloud/internal/config"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
)

// runMigrate applies the embedded migrations to the configured database.
func runMigrate(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	return postgres.Migrate(ctx, cfg.DatabaseURL)
}
