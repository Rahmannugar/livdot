package ticketing

import (
	"context"
	"testing"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/google/uuid"
)

type stubStore struct {
	purchase       Purchase
	byIdempotency  *Purchase
	byEventAndUser *Purchase
}

func (store *stubStore) EventForReservation(context.Context, string) (ReservableEvent, error) {
	return ReservableEvent{}, nil
}
func (store *stubStore) FindPurchaseByIdempotencyKey(context.Context, string, string) (Purchase, error) {
	if store.byIdempotency != nil {
		return *store.byIdempotency, nil
	}
	return Purchase{}, ErrNotFound
}
func (store *stubStore) FindPurchaseByEventAndUser(context.Context, string, string) (Purchase, error) {
	if store.byEventAndUser != nil {
		return *store.byEventAndUser, nil
	}
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
func (store *stubStore) TicketByPurchase(context.Context, string) (Ticket, error) {
	return Ticket{ID: "ticket-1"}, nil
}
func (store *stubStore) ExpireReservations(context.Context, int32) (int, error) {
	return 0, nil
}
func (store *stubStore) ActiveMembership(context.Context, string, string) (bool, error) {
	return false, nil
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

// TestPurchaseRejectsSecondPurchase proves a new idempotency key on an event the
// account already bought returns ErrAlreadyPurchased, which the handler reports
// as 409.
func TestPurchaseRejectsSecondPurchase(t *testing.T) {
	existing := Purchase{ID: "purchase-1", UserID: "user-1", EventID: "event-1"}
	service, err := NewService(
		&stubStore{byEventAndUser: &existing},
		mustProvider(t),
		&stubRefunder{},
		stubDirectory{},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.Purchase(context.Background(), uuid.NewString(), uuid.NewString(), uuid.NewString())
	if err != ErrAlreadyPurchased {
		t.Fatalf("Purchase() error = %v, want ErrAlreadyPurchased", err)
	}
}

// TestPurchaseReplayFillsCheckout proves a replayed key returns the same intent
// with the ticket fields a stored purchase row does not carry.
func TestPurchaseReplayFillsCheckout(t *testing.T) {
	existing := Purchase{ID: "purchase-1", UserID: "user-1", EventID: "event-1", Status: PurchaseInitiated}
	service, err := NewService(
		&stubStore{byIdempotency: &existing},
		mustProvider(t),
		&stubRefunder{},
		stubDirectory{},
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	replayed, err := service.Purchase(context.Background(), uuid.NewString(), uuid.NewString(), "same-key")
	if err != nil {
		t.Fatalf("Purchase() error = %v", err)
	}
	if replayed.TicketID != "ticket-1" {
		t.Fatalf("replayed TicketID = %q, want ticket-1", replayed.TicketID)
	}
	if replayed.CheckoutExpiresAt == nil {
		t.Fatal("replayed purchase should carry checkoutExpiresAt")
	}
}

func mustProvider(t *testing.T) *payment.Mock {
	t.Helper()
	provider, err := payment.NewMock("secret", "https://mock.paystack.local")
	if err != nil {
		t.Fatalf("NewMock() error = %v", err)
	}
	return provider
}
