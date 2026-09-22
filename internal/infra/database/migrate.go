package database

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
)

// the migrations directory is not present, so migrations run out of band.
var ErrMigrationsUnavailable = errors.New("migrations directory is not available")

// Migrate applies pending Tern migrations. The version table matches tern.conf
// so the CLI and the worker agree on the schema version.
func Migrate(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	if _, err := os.Stat(directory); err != nil {
		return ErrMigrationsUnavailable
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()

	migrator, err := migrate.NewMigrator(ctx, connection.Conn(), "public.schema_version")
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	if err := migrator.LoadMigrations(os.DirFS(directory)); err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	if err := migrator.Migrate(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
