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

type services struct {
	ticketing     *ticketing.Service
	notifications *notifications.Service
	finance       *finance.Service
}

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

	// startup is serialized so boot work (none today, but reserved) never runs
	// concurrently across instances. Migrations are a deploy step.
	if err := initialize(startupContext, logger, redisClient); err != nil {
		return err
	}

	directory := authrepo.NewAccountDirectory(databasePool)
	paymentProvider, err := payment.NewMock(cfg.Payment.Secret, cfg.Payment.BaseURL)
	if err != nil {
		return fmt.Errorf("configure payment provider: %w", err)
	}
	financeService, err := finance.NewService(
		financerepo.NewFinanceStore(databasePool), paymentProvider, directory)
	if err != nil {
		return fmt.Errorf("configure finance: %w", err)
	}
	ticketingService, err := ticketing.NewService(
		ticketingrepo.NewTicketStore(databasePool), paymentProvider, financeService, directory)
	if err != nil {
		return fmt.Errorf("configure ticketing: %w", err)
	}
	notificationService, err := notifications.NewService(
		notificationsrepo.NewNotificationStore(databasePool), email.NewLogSender())
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handlers := &services{
		ticketing:     ticketingService,
		notifications: notificationService,
		finance:       financeService,
	}

	// producers fire pg_notify when they queue an email; the listener drains the
	// queue on wake, and the ticker is the fallback.
	wake := make(chan struct{}, 1)
	go listen(ctx, logger, databasePool, wake)

	logger.Info("worker started", "interval", cycleInterval.String())
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker shutdown")
			return nil
		case <-ticker.C:
			runCycle(ctx, logger, handlers)
		case <-wake:
			deliver(ctx, logger, handlers)
		}
	}
}

// initialize serializes boot behind a Redis lock. Migrations are a deploy step,
// so the critical section is reserved and currently does no work.
func initialize(ctx context.Context, logger *slog.Logger, redisClient *redis.Client) error {
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
	logger.Info("worker startup lock acquired")
	return startupLock.Release(context.Background())
}

func runCycle(ctx context.Context, logger *slog.Logger, handlers *services) {
	deliver(ctx, logger, handlers)
	if expired, err := handlers.ticketing.ExpireReservations(ctx, batchSize); err != nil {
		logger.Error("reservation expiry failed", "error", err)
	} else if expired > 0 {
		logger.Info("reservations expired", "count", expired)
	}
	if settled, err := handlers.finance.ProcessPendingRefunds(ctx, batchSize); err != nil {
		logger.Error("pending refund settlement failed", "error", err)
	} else if settled > 0 {
		logger.Info("pending refunds settled", "count", settled)
	}
}

func deliver(ctx context.Context, logger *slog.Logger, handlers *services) {
	delivered, err := handlers.notifications.DeliverPending(ctx, batchSize)
	if err != nil {
		logger.Error("notification delivery failed", "error", err)
		return
	}
	if delivered > 0 {
		logger.Info("notifications delivered", "count", delivered)
	}
}

// listen holds one connection on the email channel and signals wake on every
// notification, reconnecting with a small backoff if the connection drops.
func listen(ctx context.Context, logger *slog.Logger, pool *pgxpool.Pool, wake chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		connection, err := pool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("listen acquire failed", "error", err)
			if !sleep(ctx, time.Second) {
				return
			}
			continue
		}
		if _, err := connection.Exec(ctx, "LISTEN "+notifications.Channel); err != nil {
			connection.Release()
			logger.Error("listen failed", "error", err)
			if !sleep(ctx, time.Second) {
				return
			}
			continue
		}
		logger.Info("listening for email notifications", "channel", notifications.Channel)
		for {
			if _, err := connection.Conn().WaitForNotification(ctx); err != nil {
				connection.Release()
				if ctx.Err() != nil {
					return
				}
				logger.Error("notification wait failed", "error", err)
				break
			}
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
}

func sleep(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}
