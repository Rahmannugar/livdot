package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Rahmannugar/livdot/internal/config"
	"github.com/Rahmannugar/livdot/internal/infra/database"
)

const migrateTimeout = 2 * time.Minute

func main() {
	directory := flag.String("dir", "internal/infra/database/migrations", "migrations directory")
	flag.Parse()

	if err := run(*directory); err != nil {
		slog.Error("migrate failed", "error", err)
		os.Exit(1)
	}
}

// run is the deployment migration step. It applies pending Tern migrations and
// exits, so it can run as a one-shot job before the API and worker start.
func run(directory string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), migrateTimeout)
	defer cancel()

	pool, err := database.Open(ctx, database.Config{
		Host:           cfg.Database.Host,
		Port:           cfg.Database.Port,
		User:           cfg.Database.User,
		Password:       cfg.Database.Password,
		Name:           cfg.Database.Name,
		MaxConnections: 2,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, directory); err != nil {
		return err
	}
	slog.Info("migrations applied", "directory", directory)
	return nil
}
