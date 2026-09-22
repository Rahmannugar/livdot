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
	"github.com/Rahmannugar/livdot/internal/crews"
	crewsrepo "github.com/Rahmannugar/livdot/internal/crews/repositories"
	"github.com/Rahmannugar/livdot/internal/events"
	eventsrepo "github.com/Rahmannugar/livdot/internal/events/repositories"
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
	events        *events.Service
	ticketing     *ticketing.Service
	finance       *finance.Service
	notifications *notifications.Service
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

	paymentProvider, err := payment.NewMock(cfg.Payment.Secret, cfg.Payment.BaseURL)
	if err != nil {
		return fmt.Errorf("configure payment provider: %w", err)
	}
	financeService, err := finance.NewService(financerepo.NewFinanceStore(databasePool), paymentProvider)
	if err != nil {
		return fmt.Errorf("configure finance: %w", err)
	}
	ticketingService, err := ticketing.NewService(
		ticketingrepo.NewTicketStore(databasePool), paymentProvider, financeService)
	if err != nil {
		return fmt.Errorf("configure ticketing: %w", err)
	}
	crewService, err := crews.NewService(crewsrepo.NewCrewStore(databasePool))
	if err != nil {
		return fmt.Errorf("configure crews: %w", err)
	}
	eventService, err := events.NewService(eventsrepo.NewEventStore(databasePool), crewService)
	if err != nil {
		return fmt.Errorf("configure events: %w", err)
	}
	notificationService, err := notifications.NewService(
		notificationsrepo.NewNotificationStore(databasePool),
		email.NewLogSender(),
		authrepo.NewAccountDirectory(databasePool),
	)
	if err != nil {
		return fmt.Errorf("configure notifications: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	handlers := &services{
		events:        eventService,
		ticketing:     ticketingService,
		finance:       financeService,
		notifications: notificationService,
	}
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

// runCycle derives work from the durable domain rows: notify what changed, send
// queued mail, and release lapsed reservations. Everything is idempotent, so a
// repeated cycle never duplicates a side effect.
func runCycle(ctx context.Context, logger *slog.Logger, handlers *services) {
	notifyCrewAssignments(ctx, logger, handlers)
	notifyTickets(ctx, logger, handlers)
	notifyRefunds(ctx, logger, handlers)
	if delivered, err := handlers.notifications.DeliverPending(ctx, batchSize); err != nil {
		logger.Error("notification delivery failed", "error", err)
	} else if delivered > 0 {
		logger.Info("notifications delivered", "count", delivered)
	}
	if expired, err := handlers.ticketing.ExpireReservations(ctx, batchSize); err != nil {
		logger.Error("reservation expiry failed", "error", err)
	} else if expired > 0 {
		logger.Info("reservations expired", "count", expired)
	}
}

func notifyCrewAssignments(ctx context.Context, logger *slog.Logger, handlers *services) {
	notices, err := handlers.events.PendingCrewNotices(ctx, batchSize)
	if err != nil {
		logger.Error("list crew notices failed", "error", err)
		return
	}
	for _, notice := range notices {
		err := handlers.notifications.EnqueueForAccount(ctx, notice.CrewID, "crew_assigned",
			notifications.TemplateCrewAssigned, map[string]any{
				"eventId":   notice.EventID,
				"eventName": notice.EventName,
			}, "event.assigned:"+notice.EventID)
		if err != nil {
			logger.Error("queue crew notice failed", "event_id", notice.EventID, "error", err)
			continue
		}
		if err := handlers.events.MarkCrewNotified(ctx, notice.EventID); err != nil {
			logger.Error("mark crew notified failed", "event_id", notice.EventID, "error", err)
		}
	}
}

func notifyTickets(ctx context.Context, logger *slog.Logger, handlers *services) {
	notices, err := handlers.ticketing.PendingTicketNotices(ctx, batchSize)
	if err != nil {
		logger.Error("list ticket notices failed", "error", err)
		return
	}
	for _, notice := range notices {
		err := handlers.notifications.EnqueueForAccount(ctx, notice.UserID, "ticket_issued",
			notifications.TemplateTicketReceipt, map[string]any{
				"eventId":  notice.EventID,
				"ticketId": notice.TicketID,
			}, "ticket.issued:"+notice.TicketID)
		if err != nil {
			logger.Error("queue ticket notice failed", "ticket_id", notice.TicketID, "error", err)
			continue
		}
		if err := handlers.ticketing.MarkTicketNotified(ctx, notice.TicketID); err != nil {
			logger.Error("mark ticket notified failed", "ticket_id", notice.TicketID, "error", err)
		}
	}
}

func notifyRefunds(ctx context.Context, logger *slog.Logger, handlers *services) {
	notices, err := handlers.finance.PendingRefundNotices(ctx, batchSize)
	if err != nil {
		logger.Error("list refund notices failed", "error", err)
		return
	}
	for _, notice := range notices {
		err := handlers.notifications.EnqueueForAccount(ctx, notice.UserID, "refund_completed",
			notifications.TemplateRefundReceipt, map[string]any{
				"eventId":     notice.EventID,
				"amountMinor": notice.AmountMinor,
			}, "refund.completed:"+notice.RefundID)
		if err != nil {
			logger.Error("queue refund notice failed", "refund_id", notice.RefundID, "error", err)
			continue
		}
		if err := handlers.finance.MarkRefundNotified(ctx, notice.RefundID); err != nil {
			logger.Error("mark refund notified failed", "refund_id", notice.RefundID, "error", err)
		}
	}
}
