// Package webhooks records inbound provider callbacks exactly once and routes
// them to the domain that owns the event type.
package webhooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/google/uuid"
)

var (
	ErrInvalidInput = errors.New("invalid webhook input")
	ErrDuplicate    = errors.New("webhook already recorded")
)

// an inbound callback we've verified and want to record once.
type Event struct {
	ID              string
	Provider        string
	ProviderEventID string
	Type            string
	PayloadHash     string
	Payload         []byte
	ReceivedAt      time.Time
}

// persistence port owned by the webhooks domain.
type Store interface {
	Insert(ctx context.Context, event Event) (Event, error)
	MarkProcessed(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, reason string) error
}

// a domain that processes one family of provider events.
type Handler interface {
	Handles(eventType string) bool
	Handle(ctx context.Context, event payment.WebhookEvent) error
}

type Service struct {
	store    Store
	provider payment.Provider
	handlers []Handler
	now      func() time.Time
}

func NewService(store Store, provider payment.Provider, handlers ...Handler) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("webhook store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("payment provider is required")
	}
	return &Service{store: store, provider: provider, handlers: handlers, now: time.Now}, nil
}

// Process verifies the signature, records the callback once, then hands it to
// the matching domain handler. A redelivery is a no-op so at-least-once
// provider delivery cannot double-apply a payment.
func (service *Service) Process(ctx context.Context, raw []byte, signature string) error {
	event, err := service.provider.VerifyWebhook(raw, signature)
	if err != nil {
		return err
	}
	recorded, err := service.Record(ctx, payment.ProviderName, event.ID, event.Type, raw)
	if errors.Is(err, ErrDuplicate) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, handler := range service.handlers {
		if !handler.Handles(event.Type) {
			continue
		}
		if err := handler.Handle(ctx, event); err != nil {
			_ = service.MarkFailed(ctx, recorded.ID, err.Error())
			return err
		}
		break
	}
	return service.MarkProcessed(ctx, recorded.ID)
}

// records a callback once. (provider, provider_event_id) is the dedupe key, so a
// redelivery comes back as ErrDuplicate and the caller can skip it.
func (service *Service) Record(
	ctx context.Context,
	provider, providerEventID, eventType string,
	payload []byte,
) (Event, error) {
	if strings.TrimSpace(provider) == "" || strings.TrimSpace(providerEventID) == "" ||
		strings.TrimSpace(eventType) == "" || len(payload) == 0 {
		return Event{}, ErrInvalidInput
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Event{}, fmt.Errorf("generate webhook id: %w", err)
	}
	hash := sha256.Sum256(payload)
	return service.store.Insert(ctx, Event{
		ID:              id.String(),
		Provider:        provider,
		ProviderEventID: providerEventID,
		Type:            eventType,
		PayloadHash:     hex.EncodeToString(hash[:]),
		Payload:         payload,
		ReceivedAt:      service.now(),
	})
}

func (service *Service) MarkProcessed(ctx context.Context, id string) error {
	return service.store.MarkProcessed(ctx, id)
}

func (service *Service) MarkFailed(ctx context.Context, id, reason string) error {
	return service.store.MarkFailed(ctx, id, reason)
}
