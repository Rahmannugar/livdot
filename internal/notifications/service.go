// Package notifications owns the durable email queue and its delivery.
package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/email"
)

// how long a failed delivery waits before retrying.
const retryDelay = time.Minute

var (
	ErrInvalidInput = errors.New("invalid notification input")
	ErrNotFound     = errors.New("notification not found")
)

// an email we want delivered once.
type Notification struct {
	ID                 string
	Type               string
	RecipientAccountID *string
	RecipientEmail     string
	TemplateKey        string
	Payload            []byte
	IdempotencyKey     string
	Status             string
	AttemptCount       int32
}

// what the store writes when queuing an email.
type EnqueueInput struct {
	Type               string
	RecipientAccountID *string
	RecipientEmail     string
	TemplateKey        string
	Payload            []byte
	IdempotencyKey     string
}

// persistence port owned by notifications.
type Store interface {
	Enqueue(ctx context.Context, input EnqueueInput) (Notification, bool, error)
	Claim(ctx context.Context, limit int32) ([]Notification, error)
	MarkDelivered(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id, reason string, nextAttempt time.Time) error
}

type Service struct {
	store  Store
	sender email.Sender
	now    func() time.Time
}

func NewService(store Store, sender email.Sender) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("notification store is required")
	}
	if sender == nil {
		return nil, fmt.Errorf("email sender is required")
	}
	return &Service{store: store, sender: sender, now: time.Now}, nil
}

// Enqueue records an email once. Repeated calls with the same idempotency key
// report created=false.
func (service *Service) Enqueue(ctx context.Context, input EnqueueInput) (Notification, bool, error) {
	if input.RecipientEmail == "" || input.TemplateKey == "" || input.IdempotencyKey == "" {
		return Notification{}, false, ErrInvalidInput
	}
	return service.store.Enqueue(ctx, input)
}

// DeliverPending sends claimed emails and records the outcome. A send failure is
// rescheduled with backoff instead of failing the whole batch.
func (service *Service) DeliverPending(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	items, err := service.store.Claim(ctx, limit)
	if err != nil {
		return 0, err
	}
	delivered := 0
	for _, item := range items {
		payload := decodePayload(item.Payload)
		subject, body := Render(item.TemplateKey, payload)
		message := email.Message{
			To:          item.RecipientEmail,
			TemplateKey: item.TemplateKey,
			Subject:     subject,
			Body:        body,
			Payload:     payload,
		}
		if err := service.sender.Send(ctx, message); err != nil {
			backoff := retryDelay * time.Duration(item.AttemptCount)
			if markErr := service.store.MarkFailed(ctx, item.ID, err.Error(), service.now().Add(backoff)); markErr != nil {
				return delivered, markErr
			}
			continue
		}
		if err := service.store.MarkDelivered(ctx, item.ID); err != nil {
			return delivered, err
		}
		delivered++
	}
	return delivered, nil
}

func decodePayload(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	return payload
}
