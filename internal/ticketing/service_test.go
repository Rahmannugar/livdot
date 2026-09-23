package ticketing

import (
	"context"
	"testing"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
)

type stubStore struct {
	purchase Purchase
}

func (store *stubStore) EventForReservation(context.Context, string) (ReservableEvent, error) {
	return ReservableEvent{}, nil
}
func (store *stubStore) FindPurchaseByIdempotencyKey(context.Context, string, string) (Purchase, error) {
	return Purchase{}, ErrNotFound
}
func (store *stubStore) FindPurchaseByEventAndUser(context.Context, string, string) (Purchase, error) {
	return Purchase{}, ErrNotFound
}
func (store *stubStore) FindPurchaseByID(context.Context, string) (Purchase, error) {
	return store.purchase, nil
}
func (store *stubStore) Reserve(context.Context, ReserveInput) (Purchase, Ticket, error) {
	return Purchase{}, Ticket{}, nil
}
func (store *stubStore) SetCheckoutURL(context.Context, string, string) (Purchase, error) {
	return Purchase{}, nil
}
func (store *stubStore) MarkProcessing(context.Context, string) (Purchase, error) {
	return Purchase{}, nil
}
func (store *stubStore) SettlePaid(context.Context, SettleInput) (SettleResult, error) {
	return SettleResult{Purchase: store.purchase, Ticket: &Ticket{ID: "ticket-1"}}, nil
}
func (store *stubStore) FailAndRelease(context.Context, string) (Purchase, error) {
	return Purchase{}, nil
}
func (store *stubStore) TicketByID(context.Context, string) (Ticket, error) { return Ticket{}, nil }
func (store *stubStore) ExpireReservations(context.Context, int32) (int, error) {
	return 0, nil
}

type stubDirectory struct{}

func (stubDirectory) AccountEmail(context.Context, string) (string, error) {
	return "viewer@livdot.local", nil
}

type stubRefunder struct{ called bool }

func (refunder *stubRefunder) RefundExpiredPurchase(context.Context, RefundablePurchase) error {
	refunder.called = true
	return nil
}

// TestSettlePaidGrantsAccessOnPaidCharge proves the service path resolves the
// recipient and settles without panicking. The store-level integration test does
// not cover this wiring.
func TestSettlePaidGrantsAccessOnPaidCharge(t *testing.T) {
	provider, err := payment.NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	store := &stubStore{purchase: Purchase{
		ID:          "purchase-1",
		UserID:      "user-1",
		EventID:     "event-1",
		AmountMinor: 500000,
		Provider:    "paystack",
		CreatedAt:   time.Now(),
	}}
	refunder := &stubRefunder{}
	service, err := NewService(store, provider, refunder, stubDirectory{})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	err = service.Handle(context.Background(), payment.WebhookEvent{
		Type:              payment.EventChargePaid,
		Reference:         "purchase-1",
		ProviderReference: "mock_chg_purchase-1",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if refunder.called {
		t.Fatal("a settled reservation should not trigger a refund")
	}
}
