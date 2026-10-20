package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrSchemaTooNew reports a database already migrated by a newer server version.
var ErrSchemaTooNew = errors.New("postgres: database schema is newer than this binary")

// Migrate applies the embedded migrations under a PostgreSQL session lock and refuses newer schemas.
// A malformed url yields ErrInvalidDatabaseURL.
func Migrate(ctx context.Context, url string) error {
	cfg, err := parseURL(url)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer db.Close()
	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("postgres: migrate lock: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("postgres: migrate: %w", err)
	}
	current, err := p.GetDBVersion(ctx)
	if err != nil {
		return fmt.Errorf("postgres: migrate version: %w", err)
	}
	var latest int64
	if sources := p.ListSources(); len(sources) > 0 {
		latest = sources[len(sources)-1].Version
	}
	if current > latest {
		return fmt.Errorf("%w: database at version %d, binary knows %d", ErrSchemaTooNew, current, latest)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("postgres: migrate up: %w", err)
	}
	return nil
}
