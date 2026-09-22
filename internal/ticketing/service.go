// Package ticketing owns purchases, ticket reservations, and event access.
package ticketing

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/google/uuid"
)

// how long a reserved ticket holds a slot before it lapses.
const reservationWindow = 10 * time.Minute

const (
	PurchaseInitiated  = "initiated"
	PurchaseProcessing = "processing"
	PurchasePaid       = "paid"
	PurchaseRefunded   = "refunded"
	PurchaseFailed     = "failed"
)

const (
	TicketReserved = "temporarily_reserved"
	TicketExpired  = "reservation_expired"
	TicketIssued   = "issued"
	TicketRevoked  = "revoked"
)

var (
	ErrNotFound         = errors.New("purchase or ticket not found")
	ErrInvalidInput     = errors.New("invalid ticketing input")
	ErrEventClosed      = errors.New("event is not open for purchase")
	ErrSoldOut          = errors.New("event has no tickets left")
	ErrAlreadyPurchased = errors.New("this account already has a purchase for the event")
	ErrForbidden        = errors.New("ticket belongs to another account")
	ErrReservationGone  = errors.New("reservation expired before the payment settled")
)

// a purchase as the ticketing domain sees it.
type Purchase struct {
	ID                string
	EventID           string
	UserID            string
	AmountMinor       int64
	Status            string
	Provider          string
	ProviderPaymentID *string
	IdempotencyKey    string
	CheckoutURL       *string
	TicketID          string
	PaidAt            *time.Time
	CreatedAt         time.Time
}

// a ticket as the ticketing domain sees it.
type Ticket struct {
	ID                   string
	EventID              string
	UserID               string
	Status               string
	ReservationExpiresAt time.Time
	IssuedAt             *time.Time
}

// the slice of an event needed to reserve a slot.
type ReservableEvent struct {
	ID               string
	Status           string
	AmountMinor      int64
	AvailableTickets int32
	StartsAt         time.Time
}

// what the store writes when holding a slot for a purchase.
type ReserveInput struct {
	PurchaseID     string
	TicketID       string
	EventID        string
	UserID         string
	AmountMinor    int64
	Provider       string
	IdempotencyKey string
	ReservedAt     time.Time
	ExpiresAt      time.Time
}

// what the store writes when a paid charge settles.
type SettleInput struct {
	PurchaseID        string
	ProviderPaymentID string
	MemberID          string
}

// the outcome of settling a paid charge. Expired is true when the money landed
// after the reservation lapsed, so access cannot be granted.
type SettleResult struct {
	Purchase Purchase
	Ticket   *Ticket
	Expired  bool
}

// the facts finance needs to refund a charge that settled too late.
type RefundablePurchase struct {
	PurchaseID        string
	EventID           string
	UserID            string
	AmountMinor       int64
	Provider          string
	ProviderPaymentID string
}

// finance implements this so ticketing can hand off late payments.
type Refunder interface {
	RefundExpiredPurchase(ctx context.Context, purchase RefundablePurchase) error
}

// records domain events for asynchronous delivery.
type Emitter interface {
	Enqueue(ctx context.Context, aggregateType, aggregateID, eventType, idempotencyKey string, payload any) error
}

// persistence port owned by the ticketing domain.
type Store interface {
	EventForReservation(ctx context.Context, eventID string) (ReservableEvent, error)
	FindPurchaseByIdempotencyKey(ctx context.Context, userID, key string) (Purchase, error)
	FindPurchaseByEventAndUser(ctx context.Context, eventID, userID string) (Purchase, error)
	FindPurchaseByID(ctx context.Context, id string) (Purchase, error)
	Reserve(ctx context.Context, input ReserveInput) (Purchase, Ticket, error)
	SetCheckoutURL(ctx context.Context, purchaseID, url string) (Purchase, error)
	MarkProcessing(ctx context.Context, purchaseID string) (Purchase, error)
	SettlePaid(ctx context.Context, input SettleInput) (SettleResult, error)
	FailAndRelease(ctx context.Context, purchaseID string) (Purchase, error)
	TicketByID(ctx context.Context, id string) (Ticket, error)
	ExpireReservations(ctx context.Context, limit int32) (int, error)
}

type Service struct {
	store    Store
	provider payment.Provider
	refunder Refunder
	events   Emitter
	now      func() time.Time
}

func NewService(store Store, provider payment.Provider, refunder Refunder, events Emitter) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("ticketing store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("payment provider is required")
	}
	if refunder == nil {
		return nil, fmt.Errorf("refunder is required")
	}
	if events == nil {
		return nil, fmt.Errorf("event emitter is required")
	}
	return &Service{store: store, provider: provider, refunder: refunder, events: events, now: time.Now}, nil
}

// Purchase holds a slot, records a payment intent, and returns the checkout URL.
// The idempotency key makes a retried request return the same intent.
func (service *Service) Purchase(
	ctx context.Context,
	userID, eventID, idempotencyKey string,
) (Purchase, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return Purchase{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(userID); err != nil {
		return Purchase{}, ErrInvalidInput
	}
	if _, err := uuid.Parse(eventID); err != nil {
		return Purchase{}, ErrInvalidInput
	}

	// a replayed key returns the existing intent instead of a second charge.
	if existing, err := service.store.FindPurchaseByIdempotencyKey(ctx, userID, idempotencyKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Purchase{}, err
	}
	// one ticket per user per event.
	if _, err := service.store.FindPurchaseByEventAndUser(ctx, eventID, userID); err == nil {
		return Purchase{}, ErrAlreadyPurchased
	} else if !errors.Is(err, ErrNotFound) {
		return Purchase{}, err
	}

	event, err := service.store.EventForReservation(ctx, eventID)
	if err != nil {
		return Purchase{}, err
	}
	if event.Status != "upcoming" {
		return Purchase{}, ErrEventClosed
	}
	if event.AvailableTickets <= 0 {
		return Purchase{}, ErrSoldOut
	}

	now := service.now()
	purchaseID, err := uuid.NewV7()
	if err != nil {
		return Purchase{}, fmt.Errorf("generate purchase id: %w", err)
	}
	ticketID, err := uuid.NewV7()
	if err != nil {
		return Purchase{}, fmt.Errorf("generate ticket id: %w", err)
	}
	purchase, ticket, err := service.store.Reserve(ctx, ReserveInput{
		PurchaseID:     purchaseID.String(),
		TicketID:       ticketID.String(),
		EventID:        event.ID,
		UserID:         userID,
		AmountMinor:    event.AmountMinor,
		Provider:       payment.ProviderName,
		IdempotencyKey: idempotencyKey,
		ReservedAt:     now,
		ExpiresAt:      now.Add(reservationWindow),
	})
	if err != nil {
		return Purchase{}, err
	}
	purchase.TicketID = ticket.ID

	charge, err := service.provider.InitiateCharge(ctx, payment.ChargeRequest{
		Reference:      purchase.ID,
		IdempotencyKey: idempotencyKey,
		AmountMinor:    event.AmountMinor,
		Currency:       payment.Currency,
	})
	if err != nil {
		// provider refused, so drop the hold rather than stranding the slot.
		if _, releaseErr := service.store.FailAndRelease(ctx, purchase.ID); releaseErr != nil {
			return Purchase{}, releaseErr
		}
		return Purchase{}, fmt.Errorf("initiate charge: %w", err)
	}
	return service.store.SetCheckoutURL(ctx, purchase.ID, charge.CheckoutURL)
}

// Ticket returns a ticket the caller owns.
func (service *Service) Ticket(ctx context.Context, userID, ticketID string) (Ticket, error) {
	if _, err := uuid.Parse(ticketID); err != nil {
		return Ticket{}, ErrInvalidInput
	}
	ticket, err := service.store.TicketByID(ctx, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.UserID != userID {
		return Ticket{}, ErrForbidden
	}
	return ticket, nil
}

// ExpireReservations lapses held tickets and returns their slots to the event.
// It is safe to run from multiple workers at once.
func (service *Service) ExpireReservations(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	return service.store.ExpireReservations(ctx, limit)
}

// Handles claims the charge webhook types for the shared webhook dispatcher.
func (service *Service) Handles(eventType string) bool {
	switch eventType {
	case payment.EventChargeProcessing, payment.EventChargePaid, payment.EventChargeFailed:
		return true
	default:
		return false
	}
}

func (service *Service) Handle(ctx context.Context, event payment.WebhookEvent) error {
	switch event.Type {
	case payment.EventChargeProcessing:
		_, err := service.store.MarkProcessing(ctx, event.Reference)
		return err
	case payment.EventChargePaid:
		return service.settlePaid(ctx, event)
	case payment.EventChargeFailed:
		_, err := service.store.FailAndRelease(ctx, event.Reference)
		return err
	default:
		return nil
	}
}

func (service *Service) settlePaid(ctx context.Context, event payment.WebhookEvent) error {
	memberID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate member id: %w", err)
	}
	result, err := service.store.SettlePaid(ctx, SettleInput{
		PurchaseID:        event.Reference,
		ProviderPaymentID: event.ProviderReference,
		MemberID:          memberID.String(),
	})
	if err != nil {
		return err
	}
	if !result.Expired {
		// queue the ticket email. the outbox key makes a redelivery a no-op.
		return service.events.Enqueue(ctx, "purchase", result.Purchase.ID, "ticket.issued",
			"ticket.issued:"+result.Purchase.ID, map[string]any{
				"recipientAccountId": result.Purchase.UserID,
				"notificationType":   "ticket_issued",
				"templateKey":        "ticket_receipt",
				"data": map[string]any{
					"eventId":  result.Purchase.EventID,
					"ticketId": ticketID(result.Ticket),
				},
			})
	}
	// the charge landed after the hold lapsed, so refund instead of granting access.
	return service.refunder.RefundExpiredPurchase(ctx, RefundablePurchase{
		PurchaseID:        result.Purchase.ID,
		EventID:           result.Purchase.EventID,
		UserID:            result.Purchase.UserID,
		AmountMinor:       result.Purchase.AmountMinor,
		Provider:          result.Purchase.Provider,
		ProviderPaymentID: providerPaymentID(result.Purchase.ProviderPaymentID),
	})
}

func providerPaymentID(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ticketID(ticket *Ticket) string {
	if ticket == nil {
		return ""
	}
	return ticket.ID
}
