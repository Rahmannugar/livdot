package notifications

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Rahmannugar/livdot/internal/infra/outbox"
)

// resolves the recipient email for an account.
type Directory interface {
	AccountEmail(ctx context.Context, accountID string) (string, error)
}

// the shape a domain puts in an outbox payload when it wants an email sent.
type notificationPayload struct {
	RecipientAccountID string         `json:"recipientAccountId"`
	NotificationType   string         `json:"notificationType"`
	TemplateKey        string         `json:"templateKey"`
	Data               map[string]any `json:"data"`
}

// Dispatcher turns outbox events that carry notification intent into queued
// emails. Events without that intent are ignored.
type Dispatcher struct {
	service   *Service
	directory Directory
}

func NewDispatcher(service *Service, directory Directory) *Dispatcher {
	return &Dispatcher{service: service, directory: directory}
}

func (dispatcher *Dispatcher) Handle(ctx context.Context, event outbox.Event) error {
	var payload notificationPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode notification payload: %w", err)
	}
	if payload.RecipientAccountID == "" || payload.TemplateKey == "" {
		return nil
	}
	recipient, err := dispatcher.directory.AccountEmail(ctx, payload.RecipientAccountID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(payload.Data)
	if err != nil {
		return fmt.Errorf("encode notification data: %w", err)
	}
	_, _, err = dispatcher.service.Enqueue(ctx, EnqueueInput{
		Type:               payload.NotificationType,
		RecipientAccountID: &payload.RecipientAccountID,
		RecipientEmail:     recipient,
		TemplateKey:        payload.TemplateKey,
		Payload:            data,
		IdempotencyKey:     event.IdempotencyKey,
	})
	return err
}
