// Package finance owns refunds, payouts, and the money ledger.
package finance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/Rahmannugar/livdot/internal/ticketing"
)

const (
	RefundProcessing = "processing"
	RefundRefunded   = "refunded"
	RefundFailed     = "failed"
)

var (
	ErrNotFound     = errors.New("refund or payout not found")
	ErrInvalidInput = errors.New("invalid finance input")
)

// a refund as the finance domain sees it.
type Refund struct {
	ID               string
	EventID          string
	UserID           string
	PurchaseID       string
	AmountMinor      int64
	Status           string
	Provider         string
	ProviderRefundID *string
	IdempotencyKey   string
	RefundedAt       *time.Time
}

// what the store writes when opening a refund.
type RefundInput struct {
	EventID        string
	UserID         string
	PurchaseID     string
	AmountMinor    int64
	Provider       string
	IdempotencyKey string
}

// persistence port owned by the finance domain.
type Store interface {
	CreateRefund(ctx context.Context, input RefundInput) (Refund, bool, error)
	MarkRefunded(ctx context.Context, refundID, providerRefundID string) (Refund, error)
}

type Service struct {
	store    Store
	provider payment.Provider
	now      func() time.Time
}

func NewService(store Store, provider payment.Provider) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("finance store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("payment provider is required")
	}
	return &Service{store: store, provider: provider, now: time.Now}, nil
}

// RefundExpiredPurchase refunds a charge whose reservation lapsed before the
// payment settled. The purchase id doubles as the refund idempotency key, so a
// retried webhook cannot open a second refund.
func (service *Service) RefundExpiredPurchase(ctx context.Context, purchase ticketing.RefundablePurchase) error {
	refund, created, err := service.store.CreateRefund(ctx, RefundInput{
		EventID:        purchase.EventID,
		UserID:         purchase.UserID,
		PurchaseID:     purchase.PurchaseID,
		AmountMinor:    purchase.AmountMinor,
		Provider:       purchase.Provider,
		IdempotencyKey: "refund:" + purchase.PurchaseID,
	})
	if err != nil {
		return err
	}
	if !created {
		// a refund already exists for this purchase.
		return nil
	}
	result, err := service.provider.InitiateRefund(ctx, payment.RefundRequest{
		Reference:                refund.ID,
		IdempotencyKey:           refund.IdempotencyKey,
		AmountMinor:              refund.AmountMinor,
		Currency:                 payment.Currency,
		ProviderPaymentReference: purchase.ProviderPaymentID,
	})
	if err != nil {
		return fmt.Errorf("initiate refund: %w", err)
	}
	_, err = service.store.MarkRefunded(ctx, refund.ID, result.ProviderReference)
	return err
}
