package outbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// how long a failed event waits before the publisher retries it.
const retryDelay = time.Minute

// Store is the persistence port for the publisher.
type Store interface {
	Claim(ctx context.Context, limit int32) ([]Event, error)
	MarkPublished(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id, reason string, nextAttempt time.Time) error
}

// Handler processes one outbox event.
type Handler interface {
	Handle(ctx context.Context, event Event) error
}

type Publisher struct {
	store   Store
	handler Handler
	now     func() time.Time
}

func NewPublisher(store Store, handler Handler) (*Publisher, error) {
	if store == nil {
		return nil, fmt.Errorf("outbox store is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("outbox handler is required")
	}
	return &Publisher{store: store, handler: handler, now: time.Now}, nil
}

// Publish claims due events and hands each to the handler. A handler failure is
// recorded and retried later instead of blocking the rest of the batch.
func (publisher *Publisher) Publish(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	events, err := publisher.store.Claim(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		if err := publisher.handler.Handle(ctx, event); err != nil {
			slog.Warn("outbox publish failed", "event_id", event.ID, "type", event.Type, "error", err)
			if failErr := publisher.store.MarkFailed(ctx, event.ID, err.Error(), publisher.now().Add(retryDelay)); failErr != nil {
				return 0, failErr
			}
			continue
		}
		if err := publisher.store.MarkPublished(ctx, event.ID); err != nil {
			return 0, err
		}
	}
	return len(events), nil
}
