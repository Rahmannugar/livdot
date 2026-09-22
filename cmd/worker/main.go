package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	authrepo "github.com/Rahmannugar/livdot/internal/authentication/repositories"
	"github.com/Rahmannugar/livdot/internal/config"
	"github.com/Rahmannugar/livdot/internal/finance"
	financerepo "github.com/Rahmannugar/livdot/internal/finance/repositories"
	"github.com/Rahmannugar/livdot/internal/infra/cache"
	"github.com/Rahmannugar/livdot/internal/infra/database"
	"github.com/Rahmannugar/livdot/internal/infra/email"
	"github.com/Rahmannugar/livdot/internal/infra/lock"
	"github.com/Rahmannugar/livdot/internal/infra/outbox"
	outboxrepo "github.com/Rahmannugar/livdot/internal/infra/outbox/repositories"
	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/Rahmannugar/livdot/internal/notifications"
	notificationsrepo "github.com/Rahmannugar/livdot/internal/notifications/repositories"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	ticketingrepo "github.com/Rahmannugar/livdot/internal/ticketing/repositories"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	startupTimeout  = 15 * time.Second
	batchSize       = 50
	cycleInterval   = 5 * time.Second
	startupLockKey  = "livdot:worker:startup"
	startupLockTTL  = 5 * time.Minute
	startupLockWait = 30 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	databasePool, err := database.Open(startupContext, database.Config{
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

	redisClient, err := cache.Open(startupContext, cfg.Redis.Address, cfg.Redis.Password)
	if err != nil {
		return fmt.Errorf("connect cache: %w", err)
	}
	defer redisClient.Close()

	// only one instance runs startup work; the rest wait so they never process
	// jobs against a schema that is still migrating.
	if err := initialize(startupContext, logger, databasePool, redisClient, cfg); err != nil {
		return err
	}

	paymentProvider, err := payment.NewMock(cfg.Payment.Secret, cfg.Payment.BaseURL)
	if err != nil {
		return fmt.Errorf("configure payment provider: %w", err)
	}
	outboxWriter := outbox.NewWriter(databasePool)
	financeService, err := finance.NewService(
		financerepo.NewFinanceStore(databasePool), paymentProvider, outboxWriter)
	if err != nil {
		return fmt.Errorf("configure finance: %w", err)
	}
	notificationService, err := notifications.NewService(
		notificationsrepo.NewNotificationStore(databasePool), email.NewLogSender())
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}
	dispatcher := notifications.NewDispatcher(notificationService, authrepo.NewAccountDirectory(databasePool))
	publisher, err := outbox.NewPublisher(outboxrepo.NewOutboxStore(databasePool), dispatcher)
	if err != nil {
		return fmt.Errorf("configure outbox publisher: %w", err)
	}
	ticketingService, err := ticketing.NewService(
		ticketingrepo.NewTicketStore(databasePool), paymentProvider, financeService, outboxWriter)
	if err != nil {
		return fmt.Errorf("configure ticketing: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("worker started", "interval", cycleInterval.String())
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker shutdown")
			return nil
		case <-ticker.C:
			runCycle(ctx, logger, publisher, notificationService, ticketingService)
		}
	}
}

// initialize serializes boot work behind a Redis lock. All instances run the
// same idempotent migrations, but only one at a time.
func initialize(
	ctx context.Context,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	redisClient *redis.Client,
	cfg config.Config,
) error {
	startupLock, err := lock.New(redisClient, startupLockKey, startupLockTTL)
	if err != nil {
		return fmt.Errorf("configure startup lock: %w", err)
	}

	deadline := time.Now().Add(startupLockWait)
	for {
		if err := startupLock.Acquire(ctx); err == nil {
			break
		} else if !errors.Is(err, lock.ErrNotAcquired) {
			return fmt.Errorf("acquire startup lock: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for another worker to finish startup")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	defer func() { _ = startupLock.Release(context.Background()) }()

	if err := database.Migrate(ctx, pool, cfg.MigrationsDir); err != nil {
		if errors.Is(err, database.ErrMigrationsUnavailable) {
			logger.Info("migrations directory unavailable; assuming schema is current",
				"directory", cfg.MigrationsDir)
			return nil
		}
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func runCycle(
	ctx context.Context,
	logger *slog.Logger,
	publisher *outbox.Publisher,
	notificationService *notifications.Service,
	ticketingService *ticketing.Service,
) {
	if published, err := publisher.Publish(ctx, batchSize); err != nil {
		logger.Error("outbox publish failed", "error", err)
	} else if published > 0 {
		logger.Info("outbox events published", "count", published)
	}
	if delivered, err := notificationService.DeliverPending(ctx, batchSize); err != nil {
		logger.Error("notification delivery failed", "error", err)
	} else if delivered > 0 {
		logger.Info("notifications delivered", "count", delivered)
	}
	if expired, err := ticketingService.ExpireReservations(ctx, batchSize); err != nil {
		logger.Error("reservation expiry failed", "error", err)
	} else if expired > 0 {
		logger.Info("reservations expired", "count", expired)
	}
}
