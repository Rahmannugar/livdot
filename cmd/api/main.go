package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/config"
	"github.com/Rahmannugar/livdot/internal/crews"
	crewsrepo "github.com/Rahmannugar/livdot/internal/crews/repositories"
	"github.com/Rahmannugar/livdot/internal/events"
	eventsrepo "github.com/Rahmannugar/livdot/internal/events/repositories"
	"github.com/Rahmannugar/livdot/internal/finance"
	financerepo "github.com/Rahmannugar/livdot/internal/finance/repositories"
	"github.com/Rahmannugar/livdot/internal/health"
	"github.com/Rahmannugar/livdot/internal/infra/cache"
	"github.com/Rahmannugar/livdot/internal/infra/database"
	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/Rahmannugar/livdot/internal/infra/ratelimit"
	streamprovider "github.com/Rahmannugar/livdot/internal/infra/streaming"
	"github.com/Rahmannugar/livdot/internal/streaming"
	streamingrepo "github.com/Rahmannugar/livdot/internal/streaming/repositories"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	ticketingrepo "github.com/Rahmannugar/livdot/internal/ticketing/repositories"
	"github.com/Rahmannugar/livdot/internal/webhooks"
	webhooksrepo "github.com/Rahmannugar/livdot/internal/webhooks/repositories"
	"github.com/gin-gonic/gin"
)

const (
	startupTimeout    = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	if cfg.Environment != config.EnvironmentDevelopment {
		gin.SetMode(gin.ReleaseMode)
	}

	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	databasePool, err := database.Open(
		startupContext,
		database.Config{
			Host:           cfg.Database.Host,
			Port:           cfg.Database.Port,
			User:           cfg.Database.User,
			Password:       cfg.Database.Password,
			Name:           cfg.Database.Name,
			MaxConnections: cfg.Database.MaxConnections,
		},
	)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer databasePool.Close()

	redisClient, err := cache.Open(
		startupContext,
		cfg.Redis.Address,
		cfg.Redis.Password,
	)
	if err != nil {
		return fmt.Errorf("connect cache: %w", err)
	}
	defer redisClient.Close()

	router := gin.New()
	router.Use(gin.Recovery())
	health.RegisterRoutes(router, databasePool, redisClient)

	limiter, err := ratelimit.New(redisClient, "livdot:ratelimit", accountSubject)
	if err != nil {
		return fmt.Errorf("configure rate limiter: %w", err)
	}

	authService, err := authentication.NewService(
		databasePool,
		redisClient,
		cfg.Session.Lifetime,
		cfg.Session.CacheTTL,
	)
	if err != nil {
		return fmt.Errorf("configure authentication: %w", err)
	}
	authentication.RegisterRoutes(router, authService, limiter)

	crewService, err := crews.NewService(crewsrepo.NewCrewStore(databasePool))
	if err != nil {
		return fmt.Errorf("configure crews: %w", err)
	}
	eventService, err := events.NewService(
		eventsrepo.NewEventStore(databasePool), crewService)
	if err != nil {
		return fmt.Errorf("configure events: %w", err)
	}
	paymentProvider, err := payment.NewMock(cfg.Payment.Secret, cfg.Payment.BaseURL)
	if err != nil {
		return fmt.Errorf("configure payment provider: %w", err)
	}
	financeService, err := finance.NewService(
		financerepo.NewFinanceStore(databasePool), paymentProvider)
	if err != nil {
		return fmt.Errorf("configure finance: %w", err)
	}
	ticketingService, err := ticketing.NewService(
		ticketingrepo.NewTicketStore(databasePool),
		paymentProvider,
		financeService,
	)
	if err != nil {
		return fmt.Errorf("configure ticketing: %w", err)
	}
	webhookService, err := webhooks.NewService(
		webhooksrepo.NewWebhookStore(databasePool),
		paymentProvider,
		ticketingService,
	)
	if err != nil {
		return fmt.Errorf("configure webhooks: %w", err)
	}

	streamProvider, err := streamprovider.NewMock(cfg.Streaming.BaseURL)
	if err != nil {
		return fmt.Errorf("configure streaming provider: %w", err)
	}
	streamService, err := streaming.NewService(
		streamingrepo.NewStreamStore(databasePool),
		streamProvider,
		financeService,
	)
	if err != nil {
		return fmt.Errorf("configure streaming: %w", err)
	}

	// public browse stays open; mutations resolve the session first so
	// RequireRole can check the identity.
	public := router.Group("/api")
	authenticated := router.Group("/api")
	authenticated.Use(authentication.RequireSession(authService))
	crews.RegisterRoutes(public, authenticated, crewService, limiter)
	events.RegisterRoutes(public, authenticated, eventService, limiter)
	ticketing.RegisterRoutes(authenticated, ticketingService, limiter)
	streaming.RegisterRoutes(authenticated, streamService, limiter)
	finance.RegisterRoutes(authenticated, financeService, limiter)
	webhooks.RegisterRoutes(public, webhookService, limiter)
	if cfg.Environment != config.EnvironmentProduction {
		webhooks.RegisterDevSimulator(public, paymentProvider, webhookService, limiter)
	}

	server := &http.Server{
		Addr:              cfg.HTTP.Address(),
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", server.Addr, "environment", cfg.Environment)
		serveErr := server.ListenAndServe()
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		serverErrors <- serveErr
	}()

	shutdownSignal, stop := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stop()

	select {
	case serveErr := <-serverErrors:
		if serveErr != nil {
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
		return nil
	case <-shutdownSignal.Done():
		logger.Info("api shutdown started")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	if err := <-serverErrors; err != nil {
		return fmt.Errorf("stop HTTP server: %w", err)
	}

	logger.Info("api shutdown completed")
	return nil
}

// accountSubject keys account-scoped rate limits by the authenticated account,
// falling back to the client IP when there is no session.
func accountSubject(ctx *gin.Context) string {
	if identity, ok := authentication.IdentityFrom(ctx); ok {
		return identity.AccountID
	}
	return ""
}
