package webhooks

import (
	"context"
	"testing"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
)

type stubStore struct {
	seen      map[string]bool
	processed int
}

func (store *stubStore) Insert(_ context.Context, event Event) (Event, error) {
	key := event.Provider + "|" + event.ProviderEventID
	if store.seen[key] {
		return Event{}, ErrDuplicate
	}
	store.seen[key] = true
	return event, nil
}

func (store *stubStore) MarkProcessed(context.Context, string) error {
	store.processed++
	return nil
}

func (store *stubStore) MarkFailed(context.Context, string, string) error { return nil }

type spyHandler struct {
	calls int
}

func (handler *spyHandler) Handles(eventType string) bool {
	return eventType == payment.EventChargePaid
}

func (handler *spyHandler) Handle(context.Context, payment.WebhookEvent) error {
	handler.calls++
	return nil
}

// TestProcessDeduplicatesProviderDelivery covers the webhook-duplication
// criterion: a redelivered callback is a no-op and cannot apply a payment twice.
func TestProcessDeduplicatesProviderDelivery(t *testing.T) {
	provider, err := payment.NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	store := &stubStore{seen: map[string]bool{}}
	handler := &spyHandler{}
	service, err := NewService(store, provider, handler)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	payload, signature := provider.EncodeWebhook(payment.WebhookEvent{
		ID:        "evt_1",
		Type:      payment.EventChargePaid,
		Reference: "purchase-1",
	})
	ctx := context.Background()

	if err := service.Process(ctx, payload, signature); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	if err := service.Process(ctx, payload, signature); err != nil {
		t.Fatalf("second Process() error = %v", err)
	}
	if handler.calls != 1 {
		t.Fatalf("handler calls = %d, want 1", handler.calls)
	}
	if store.processed != 1 {
		t.Fatalf("processed = %d, want 1", store.processed)
	}
}

// TestProcessRejectsForgedSignature covers signature verification.
func TestProcessRejectsForgedSignature(t *testing.T) {
	provider, err := payment.NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	service, err := NewService(&stubStore{seen: map[string]bool{}}, provider, &spyHandler{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	payload, _ := provider.EncodeWebhook(payment.WebhookEvent{
		ID: "evt_1", Type: payment.EventChargePaid, Reference: "purchase-1",
	})

	if err := service.Process(context.Background(), payload, "forged"); err != payment.ErrInvalidSignature {
		t.Fatalf("Process() error = %v, want ErrInvalidSignature", err)
	}
}
