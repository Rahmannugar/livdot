package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	authenticationdb "github.com/Rahmannugar/livdot/internal/authentication/repositories/generated"
	"github.com/Rahmannugar/livdot/internal/config"
	"github.com/Rahmannugar/livdot/internal/infra/cache"
	"github.com/Rahmannugar/livdot/internal/infra/database"
)

const commandTimeout = 15 * time.Second

func main() {
	email := flag.String("email", "", "internal admin email")
	password := flag.String("password", "", "internal admin password")
	fullName := flag.String("full-name", "", "internal admin full name")
	role := flag.String("role", string(authenticationdb.InternalAdminRoleAdmin), "internal admin role: admin or subadmin")
	flag.Parse()

	if err := run(*email, *password, *fullName, *role); err != nil {
		slog.Error("create internal admin failed", "error", err)
		os.Exit(1)
	}
}

func run(email, password, fullName, role string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	databasePool, err := database.Open(ctx, database.Config{
		Host:           cfg.Database.Host,
		Port:           cfg.Database.Port,
		User:           cfg.Database.User,
		Password:       cfg.Database.Password,
		Name:           cfg.Database.Name,
		MaxConnections: cfg.Database.MaxConnections,
	})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer databasePool.Close()

	redisClient, err := cache.Open(ctx, cfg.Redis.Address, cfg.Redis.Password)
	if err != nil {
		return fmt.Errorf("connect cache: %w", err)
	}
	defer redisClient.Close()

	service, err := authentication.NewService(
		databasePool, redisClient, cfg.Session.Lifetime, cfg.Session.CacheTTL)
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	if err := service.CreateInternalAdmin(ctx, authentication.InternalAdminInput{
		Email:    email,
		Password: password,
		FullName: fullName,
		Role:     authenticationdb.InternalAdminRole(role),
	}); err != nil {
		return fmt.Errorf("create internal admin: %w", err)
	}
	slog.Info("internal admin created", "email", email, "role", role)
	return nil
}
