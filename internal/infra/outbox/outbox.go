// Package outbox durably records domain events for asynchronous delivery.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	outboxdb "github.com/Rahmannugar/livdot/internal/infra/outbox/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// an outbox event already exists for this idempotency key.
var ErrDuplicate = errors.New("outbox event already recorded")

// one recorded domain event.
type Event struct {
	ID             string
	AggregateType  string
	AggregateID    string
	Type           string
	Payload        []byte
	IdempotencyKey string
	AttemptCount   int32
	CreatedAt      time.Time
}

// Writer records events inside the caller's transaction so an event commits
// with the state change that produced it.
type Writer struct {
	db outboxdb.DBTX
}

func NewWriter(db outboxdb.DBTX) *Writer {
	return &Writer{db: db}
}

func (writer *Writer) Enqueue(
	ctx context.Context,
	aggregateType, aggregateID, eventType, idempotencyKey string,
	payload any,
) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate outbox id: %w", err)
	}
	aggregate, err := uuid.Parse(aggregateID)
	if err != nil {
		return fmt.Errorf("parse outbox aggregate id: %w", err)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode outbox payload: %w", err)
	}
	if _, err := outboxdb.New(writer.db).EnqueueOutboxEvent(ctx, outboxdb.EnqueueOutboxEventParams{
		ID:             id,
		AggregateType:  aggregateType,
		AggregateID:    aggregate,
		EventType:      eventType,
		Payload:        body,
		IdempotencyKey: idempotencyKey,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrDuplicate
		}
		return fmt.Errorf("enqueue outbox event: %w", err)
	}
	return nil
}
