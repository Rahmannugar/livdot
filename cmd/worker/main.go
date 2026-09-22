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
	"github.com/Rahmannugar/livdot/internal/infra/stream"
	"github.com/Rahmannugar/livdot/internal/notifications"
	notificationsrepo "github.com/Rahmannugar/livdot/internal/notifications/repositories"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	ticketingrepo "github.com/Rahmannugar/livdot/internal/ticketing/repositories"
	"github.com/redis/go-redis/v9"
)

const (
	startupTimeout  = 15 * time.Second
	batchSize       = 50
	cycleInterval   = 5 * time.Second
	streamBlock     = time.Second
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

	// startup is serialized so boot work never runs
	// concurrently across instances.
	if err := initialize(startupContext, logger, redisClient); err != nil {
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
	bridge, err := stream.NewBridge(redisClient, stream.DefaultName)
	if err != nil {
		return fmt.Errorf("configure stream bridge: %w", err)
	}
	publisher, err := outbox.NewPublisher(outboxrepo.NewOutboxStore(databasePool), bridge)
	if err != nil {
		return fmt.Errorf("configure outbox publisher: %w", err)
	}
	consumer, err := stream.NewConsumer(redisClient, stream.DefaultName, stream.DefaultGroup, consumerName())
	if err != nil {
		return fmt.Errorf("configure stream consumer: %w", err)
	}
	if err := consumer.EnsureGroup(startupContext); err != nil {
		return err
	}
	ticketingService, err := ticketing.NewService(
		ticketingrepo.NewTicketStore(databasePool), paymentProvider, financeService, outboxWriter)
	if err != nil {
		return fmt.Errorf("configure ticketing: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger.Info("worker started", "interval", cycleInterval.String(), "consumer", consumerName())
	ticker := time.NewTicker(cycleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("worker shutdown")
			return nil
		case <-ticker.C:
			runCycle(ctx, logger, publisher, consumer, dispatcher, notificationService, ticketingService)
		}
	}
}

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
	// reserved for one-time boot work; nothing to run yet.
	return startupLock.Release(context.Background())
}

func runCycle(
	ctx context.Context,
	logger *slog.Logger,
	publisher *outbox.Publisher,
	consumer *stream.Consumer,
	dispatcher *notifications.Dispatcher,
	notificationService *notifications.Service,
	ticketingService *ticketing.Service,
) {
	if published, err := publisher.Publish(ctx, batchSize); err != nil {
		logger.Error("outbox publish failed", "error", err)
	} else if published > 0 {
		logger.Info("outbox events published", "count", published)
	}
	if consumed, err := consumer.Consume(ctx, batchSize, dispatcher, streamBlock); err != nil {
		logger.Error("stream consume failed", "error", err)
	} else if consumed > 0 {
		logger.Info("stream events consumed", "count", consumed)
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

func consumerName() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "worker"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
