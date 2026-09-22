package notificationsrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/notifications"
	notificationsdb "github.com/Rahmannugar/livdot/internal/notifications/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type NotificationStore struct {
	pool *pgxpool.Pool
}

func NewNotificationStore(pool *pgxpool.Pool) *NotificationStore {
	return &NotificationStore{pool: pool}
}

// Enqueue inserts an email once. created=false means the idempotency key was
// already used.
func (store *NotificationStore) Enqueue(ctx context.Context, input notifications.EnqueueInput) (notifications.Notification, bool, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return notifications.Notification{}, false, fmt.Errorf("generate notification id: %w", err)
	}
	record, err := notificationsdb.New(store.pool).CreateEmailNotification(ctx, notificationsdb.CreateEmailNotificationParams{
		ID:               id,
		NotificationType: input.Type,
		RecipientUserID:  optionalUUID(input.RecipientAccountID),
		RecipientEmail:   input.RecipientEmail,
		TemplateKey:      input.TemplateKey,
		Payload:          input.Payload,
		IdempotencyKey:   input.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return notifications.Notification{}, false, nil
	}
	if err != nil {
		return notifications.Notification{}, false, fmt.Errorf("create email notification: %w", err)
	}
	return notification(record), true, nil
}

func (store *NotificationStore) Claim(ctx context.Context, limit int32) ([]notifications.Notification, error) {
	records, err := notificationsdb.New(store.pool).ClaimPendingEmailNotifications(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("claim email notifications: %w", err)
	}
	items := make([]notifications.Notification, 0, len(records))
	for _, record := range records {
		items = append(items, notification(record))
	}
	return items, nil
}

func (store *NotificationStore) MarkDelivered(ctx context.Context, id string) error {
	notificationID, err := uuid.Parse(id)
	if err != nil {
		return notifications.ErrNotFound
	}
	if _, err := notificationsdb.New(store.pool).MarkEmailNotificationDelivered(ctx, notificationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark notification delivered: %w", err)
	}
	return nil
}

func (store *NotificationStore) MarkFailed(ctx context.Context, id, reason string, nextAttempt time.Time) error {
	notificationID, err := uuid.Parse(id)
	if err != nil {
		return notifications.ErrNotFound
	}
	lastError := reason
	if _, err := notificationsdb.New(store.pool).MarkEmailNotificationFailed(ctx, notificationsdb.MarkEmailNotificationFailedParams{
		ID:            notificationID,
		LastError:     &lastError,
		NextAttemptAt: pgtype.Timestamptz{Time: nextAttempt, Valid: true},
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark notification failed: %w", err)
	}
	return nil
}

func notification(record notificationsdb.EmailNotification) notifications.Notification {
	var accountID *string
	if record.RecipientUserID.Valid {
		value := uuid.UUID(record.RecipientUserID.Bytes).String()
		accountID = &value
	}
	return notifications.Notification{
		ID:                 record.ID.String(),
		Type:               record.NotificationType,
		RecipientAccountID: accountID,
		RecipientEmail:     record.RecipientEmail,
		TemplateKey:        record.TemplateKey,
		Payload:            record.Payload,
		IdempotencyKey:     record.IdempotencyKey,
		Status:             string(record.Status),
		AttemptCount:       record.AttemptCount,
	}
}

func optionalUUID(value *string) pgtype.UUID {
	if value == nil || *value == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}
