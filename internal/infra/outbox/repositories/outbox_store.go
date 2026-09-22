package outboxrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/outbox"
	outboxdb "github.com/Rahmannugar/livdot/internal/infra/outbox/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type OutboxStore struct {
	pool *pgxpool.Pool
}

func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{pool: pool}
}

func (store *OutboxStore) Claim(ctx context.Context, limit int32) ([]outbox.Event, error) {
	records, err := outboxdb.New(store.pool).ClaimPendingOutboxEvents(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	events := make([]outbox.Event, 0, len(records))
	for _, record := range records {
		events = append(events, event(record))
	}
	return events, nil
}

func (store *OutboxStore) MarkPublished(ctx context.Context, id string) error {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return outbox.ErrDuplicate
	}
	if _, err := outboxdb.New(store.pool).MarkOutboxEventPublished(ctx, eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark outbox published: %w", err)
	}
	return nil
}

func (store *OutboxStore) MarkFailed(ctx context.Context, id, reason string, nextAttempt time.Time) error {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return outbox.ErrDuplicate
	}
	lastError := reason
	if _, err := outboxdb.New(store.pool).MarkOutboxEventFailed(ctx, outboxdb.MarkOutboxEventFailedParams{
		ID:            eventID,
		LastError:     &lastError,
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttempt, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark outbox failed: %w", err)
	}
	return nil
}

func event(record outboxdb.OutboxEvent) outbox.Event {
	return outbox.Event{
		ID:             record.ID.String(),
		AggregateType:  record.AggregateType,
		AggregateID:    record.AggregateID.String(),
		Type:           record.EventType,
		Payload:        record.Payload,
		IdempotencyKey: record.IdempotencyKey,
		AttemptCount:   record.AttemptCount,
		CreatedAt:      record.CreatedAt.Time,
	}
}
