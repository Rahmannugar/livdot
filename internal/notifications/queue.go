package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	notificationsdb "github.com/Rahmannugar/livdot/internal/notifications/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Channel is the PostgreSQL NOTIFY channel the worker listens on. Producers fire
// it inside the same transaction that inserts the email row, so the wake is on
// commit and cannot be lost.
const Channel = "livdot_email"

// Email is an email a domain wants queued. The constructors below keep the
// template and payload shape in one place.
type Email struct {
	RecipientEmail     string
	RecipientAccountID string
	Type               string
	TemplateKey        string
	IdempotencyKey     string
	Data               map[string]any
}

// Queue writes email rows inside a caller-owned transaction.
type Queue struct {
	db notificationsdb.DBTX
}

func NewQueue(db notificationsdb.DBTX) *Queue {
	return &Queue{db: db}
}

// Enqueue inserts the email and wakes the worker. It returns false when the
// idempotency key was already used, so a replayed action never double-sends.
func (queue *Queue) Enqueue(ctx context.Context, message Email) (bool, error) {
	if strings.TrimSpace(message.RecipientEmail) == "" ||
		strings.TrimSpace(message.IdempotencyKey) == "" ||
		strings.TrimSpace(message.TemplateKey) == "" {
		return false, ErrInvalidInput
	}
	id, err := uuid.NewV7()
	if err != nil {
		return false, fmt.Errorf("generate notification id: %w", err)
	}
	payload, err := json.Marshal(message.Data)
	if err != nil {
		return false, fmt.Errorf("encode notification data: %w", err)
	}
	if _, err := notificationsdb.New(queue.db).CreateEmailNotification(ctx, notificationsdb.CreateEmailNotificationParams{
		ID:                 id,
		NotificationType:   message.Type,
		RecipientAccountID: optionalUUID(message.RecipientAccountID),
		RecipientEmail:     message.RecipientEmail,
		TemplateKey:        message.TemplateKey,
		Payload:            payload,
		IdempotencyKey:     message.IdempotencyKey,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("create email notification: %w", err)
	}
	if _, err := queue.db.Exec(ctx, "SELECT pg_notify($1, $2)", Channel, id.String()); err != nil {
		return true, fmt.Errorf("notify email queue: %w", err)
	}
	return true, nil
}

func optionalUUID(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
