package webhooksrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Rahmannugar/livdot/internal/webhooks"
	webhooksdb "github.com/Rahmannugar/livdot/internal/webhooks/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookStore struct {
	pool *pgxpool.Pool
}

func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore {
	return &WebhookStore{pool: pool}
}

func (store *WebhookStore) Insert(ctx context.Context, event webhooks.Event) (webhooks.Event, error) {
	id, err := uuid.Parse(event.ID)
	if err != nil {
		return webhooks.Event{}, webhooks.ErrInvalidInput
	}
	record, err := webhooksdb.New(store.pool).InsertWebhookEvent(ctx, webhooksdb.InsertWebhookEventParams{
		ID:              id,
		Provider:        event.Provider,
		ProviderEventID: event.ProviderEventID,
		EventType:       event.Type,
		PayloadHash:     event.PayloadHash,
		Payload:         event.Payload,
	})
	// no row means (provider, provider_event_id) is already recorded.
	if errors.Is(err, pgx.ErrNoRows) {
		return webhooks.Event{}, webhooks.ErrDuplicate
	}
	if err != nil {
		return webhooks.Event{}, fmt.Errorf("insert webhook event: %w", err)
	}
	return webhookEvent(record), nil
}

func (store *WebhookStore) MarkProcessed(ctx context.Context, id string) error {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return webhooks.ErrInvalidInput
	}
	if _, err := webhooksdb.New(store.pool).MarkWebhookProcessed(ctx, eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark webhook processed: %w", err)
	}
	return nil
}

func (store *WebhookStore) MarkFailed(ctx context.Context, id, reason string) error {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return webhooks.ErrInvalidInput
	}
	var lastError *string
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		lastError = &trimmed
	}
	if _, err := webhooksdb.New(store.pool).MarkWebhookFailed(ctx, webhooksdb.MarkWebhookFailedParams{
		ID:        eventID,
		LastError: lastError,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark webhook failed: %w", err)
	}
	return nil
}

func webhookEvent(record webhooksdb.WebhookEvent) webhooks.Event {
	return webhooks.Event{
		ID:              record.ID.String(),
		Provider:        record.Provider,
		ProviderEventID: record.ProviderEventID,
		Type:            record.EventType,
		PayloadHash:     record.PayloadHash,
		Payload:         record.Payload,
		ReceivedAt:      record.ReceivedAt.Time,
	}
}
