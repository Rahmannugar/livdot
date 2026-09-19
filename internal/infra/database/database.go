package database

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host           string
	Port           uint16
	User           string
	Password       string
	Name           string
	MaxConnections int32
}

func Open(ctx context.Context, settings Config) (*pgxpool.Pool, error) {
	connectionURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(settings.User, settings.Password),
		Host:   net.JoinHostPort(settings.Host, strconv.Itoa(int(settings.Port))),
		Path:   settings.Name,
	}
	query := connectionURL.Query()
	query.Set("sslmode", "disable")
	connectionURL.RawQuery = query.Encode()

	config, err := pgxpool.ParseConfig(connectionURL.String())
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}

	// Bound each process so horizontally scaled API instances cannot exhaust PostgreSQL.
	config.MaxConns = settings.MaxConnections

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	return pool, nil
}
