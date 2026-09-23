// Package finance owns refunds, payouts, and the money ledger.
package finance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/payment"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	"github.com/google/uuid"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

const (
	RefundProcessing = "processing"
	RefundRefunded   = "refunded"
	RefundFailed     = "failed"
)

const (
	PayoutProcessing = "processing"
	PayoutPaid       = "paid"
	PayoutFailed     = "failed"
)

var (
	ErrNotFound      = errors.New("refund or payout not found")
	ErrInvalidInput  = errors.New("invalid finance input")
	ErrForbidden     = errors.New("this record belongs to another account")
	ErrNotRefundable = errors.New("only a failed stream can be refunded")
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

// a payout as the finance domain sees it.
type Payout struct {
	ID               string
	EventID          string
	HostID           string
	AmountMinor      int64
	Status           string
	Provider         string
	ProviderPayoutID *string
	IdempotencyKey   string
	PaidAt           *time.Time
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

// what the store writes when opening a payout.
type PayoutInput struct {
	EventID        string
	HostID         string
	AmountMinor    int64
	Provider       string
	IdempotencyKey string
}

// one paid purchase that may need a refund.
type PurchaseForRefund struct {
	PurchaseID        string
	UserID            string
	AmountMinor       int64
	Provider          string
	ProviderPaymentID *string
	AlreadyRefunded   bool
}

// resolves a recipient email so a receipt can be queued inside the transaction.
type Directory interface {
	AccountEmail(ctx context.Context, accountID string) (string, error)
}

// the event facts finance needs for a payout.
type Event struct {
	ID     string
	HostID string
	Status string
}

// one ledger movement.
type LedgerInput struct {
	EventID     string
	EntryType   string
	AmountMinor int64
	PurchaseID  *string
	RefundID    *string
	PayoutID    *string
}

type RefundFilter struct {
	EventID  *string
	UserID   *string
	Cursor   *string
	PageSize int32
}

type PayoutFilter struct {
	EventID  *string
	HostID   *string
	Cursor   *string
	PageSize int32
}

type RefundPage struct {
	Refunds    []Refund
	NextCursor string
}

type PayoutPage struct {
	Payouts    []Payout
	NextCursor string
}

// persistence port owned by the finance domain.
type Store interface {
	CreateRefund(ctx context.Context, input RefundInput) (Refund, bool, error)
	SettleRefund(ctx context.Context, refundID, providerRefundID, recipientEmail string) (Refund, error)
	RefundByPurchaseID(ctx context.Context, purchaseID string) (Refund, error)
	PaidPurchasesForEvent(ctx context.Context, eventID string) ([]PurchaseForRefund, error)
	SumPaidPurchases(ctx context.Context, eventID string) (int64, error)
	SumRefundedPurchases(ctx context.Context, eventID string) (int64, error)
	StreamStatus(ctx context.Context, eventID string) (string, error)
	EventForFinance(ctx context.Context, eventID string) (Event, error)
	RefundByID(ctx context.Context, id string) (Refund, error)
	Refunds(ctx context.Context, filter RefundFilter) ([]Refund, error)
	CreatePayout(ctx context.Context, input PayoutInput) (Payout, bool, error)
	MarkPayoutPaid(ctx context.Context, payoutID, providerPayoutID string) (Payout, error)
	PayoutByID(ctx context.Context, id string) (Payout, error)
	Payouts(ctx context.Context, filter PayoutFilter) ([]Payout, error)
	RecordLedger(ctx context.Context, input LedgerInput) error
	ClaimPendingRefunds(ctx context.Context, limit int32) ([]Refund, error)
	PurchaseForRefund(ctx context.Context, purchaseID string) (PurchaseForRefund, error)
}

type Service struct {
	store     Store
	provider  payment.Provider
	directory Directory
	now       func() time.Time
}

func NewService(store Store, provider payment.Provider, directory Directory) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("finance store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("payment provider is required")
	}
	if directory == nil {
		return nil, fmt.Errorf("recipient directory is required")
	}
	return &Service{store: store, provider: provider, directory: directory, now: time.Now}, nil
}

// recipientEmail is best-effort: a missing address must not block a refund.
func (service *Service) recipientEmail(ctx context.Context, accountID string) string {
	value, err := service.directory.AccountEmail(ctx, accountID)
	if err != nil {
		return ""
	}
	return value
}

// RefundExpiredPurchase refunds a charge whose reservation lapsed before the
// payment settled. The purchase id is the refund idempotency key, so a retried
// webhook cannot open a second refund.
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
		return nil
	}
	providerPaymentID := purchase.ProviderPaymentID
	return service.settleRefund(ctx, refund, &providerPaymentID, service.recipientEmail(ctx, purchase.UserID))
}

// RefundEvent refunds every paid purchase on an event. CreateRefund is unique
// per purchase, so repeated calls (auto trigger and admin review) never pay
// twice. A refund already in processing (a prior settlement that failed) is
// retried rather than skipped. automatic records whether the 25% rule or an
// admin drove it.
func (service *Service) RefundEvent(ctx context.Context, eventID string, automatic bool) (int, error) {
	// an admin may only refund an event whose stream actually failed.
	if !automatic {
		status, err := service.store.StreamStatus(ctx, eventID)
		if err != nil {
			return 0, err
		}
		if status != "failed" {
			return 0, ErrNotRefundable
		}
	}
	purchases, err := service.store.PaidPurchasesForEvent(ctx, eventID)
	if err != nil {
		return 0, err
	}
	refunded := 0
	for _, purchase := range purchases {
		// already fully refunded; nothing to do.
		if purchase.AlreadyRefunded {
			continue
		}
		refund, created, err := service.store.CreateRefund(ctx, RefundInput{
			EventID:        eventID,
			UserID:         purchase.UserID,
			PurchaseID:     purchase.PurchaseID,
			AmountMinor:    purchase.AmountMinor,
			Provider:       purchase.Provider,
			IdempotencyKey: "refund:" + purchase.PurchaseID,
		})
		if err != nil {
			return refunded, err
		}
		if !created {
			// the row exists but is stuck in processing from a prior failed
			// settlement; load it and retry the settlement.
			existing, lookupErr := service.store.RefundByPurchaseID(ctx, purchase.PurchaseID)
			if lookupErr != nil {
				return refunded, lookupErr
			}
			if existing.Status != RefundProcessing {
				continue
			}
			refund = existing
		}
		if err := service.settleRefund(ctx, refund, purchase.ProviderPaymentID, service.recipientEmail(ctx, purchase.UserID)); err != nil {
			return refunded, err
		}
		refunded++
	}
	slog.Info("event refunds processed", "event_id", eventID, "automatic", automatic, "count", refunded)
	return refunded, nil
}

// ProcessPendingRefunds retries refunds stuck in processing (a settlement that
// failed after the row was created). It is safe to run from multiple workers.
func (service *Service) ProcessPendingRefunds(ctx context.Context, limit int32) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	refunds, err := service.store.ClaimPendingRefunds(ctx, limit)
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, refund := range refunds {
		purchase, err := service.store.PurchaseForRefund(ctx, refund.PurchaseID)
		if err != nil {
			slog.Warn("load purchase for pending refund", "refund_id", refund.ID, "error", err)
			continue
		}
		if err := service.settleRefund(ctx, refund, purchase.ProviderPaymentID, service.recipientEmail(ctx, refund.UserID)); err != nil {
			slog.Warn("settle pending refund", "refund_id", refund.ID, "error", err)
			continue
		}
		processed++
	}
	return processed, nil
}

// AccruePayout pays the host the net of paid purchases minus completed refunds.
// The event id is the payout idempotency key, so this is safe to retry.
func (service *Service) AccruePayout(ctx context.Context, eventID string) error {
	paid, err := service.store.SumPaidPurchases(ctx, eventID)
	if err != nil {
		return err
	}
	refunded, err := service.store.SumRefundedPurchases(ctx, eventID)
	if err != nil {
		return err
	}
	net := paid - refunded
	if net <= 0 {
		// nothing left to pay the host.
		return nil
	}
	event, err := service.store.EventForFinance(ctx, eventID)
	if err != nil {
		return err
	}
	payout, created, err := service.store.CreatePayout(ctx, PayoutInput{
		EventID:        eventID,
		HostID:         event.HostID,
		AmountMinor:    net,
		Provider:       payment.ProviderName,
		IdempotencyKey: "payout:" + eventID,
	})
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	result, err := service.provider.InitiatePayout(ctx, payment.PayoutRequest{
		Reference:      payout.ID,
		IdempotencyKey: payout.IdempotencyKey,
		AmountMinor:    payout.AmountMinor,
		Currency:       payment.Currency,
	})
	if err != nil {
		return fmt.Errorf("initiate payout: %w", err)
	}
	marked, err := service.store.MarkPayoutPaid(ctx, payout.ID, result.ProviderReference)
	if err != nil {
		return err
	}
	return service.store.RecordLedger(ctx, LedgerInput{
		EventID:     eventID,
		EntryType:   "payout",
		AmountMinor: marked.AmountMinor,
		PayoutID:    &marked.ID,
	})
}

func (service *Service) Refund(ctx context.Context, id string) (Refund, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Refund{}, ErrNotFound
	}
	return service.store.RefundByID(ctx, id)
}

func (service *Service) Refunds(ctx context.Context, filter RefundFilter) (RefundPage, error) {
	normalizeFilter(&filter.PageSize, &filter.Cursor)
	pageSize := filter.PageSize
	filter.PageSize = pageSize + 1
	refunds, err := service.store.Refunds(ctx, filter)
	if err != nil {
		return RefundPage{}, err
	}
	return refundPage(pageSize, refunds), nil
}

func (service *Service) Payout(ctx context.Context, id string) (Payout, error) {
	if _, err := uuid.Parse(id); err != nil {
		return Payout{}, ErrNotFound
	}
	return service.store.PayoutByID(ctx, id)
}

func (service *Service) Payouts(ctx context.Context, filter PayoutFilter) (PayoutPage, error) {
	normalizeFilter(&filter.PageSize, &filter.Cursor)
	pageSize := filter.PageSize
	filter.PageSize = pageSize + 1
	payouts, err := service.store.Payouts(ctx, filter)
	if err != nil {
		return PayoutPage{}, err
	}
	return payoutPage(pageSize, payouts), nil
}

func (service *Service) settleRefund(ctx context.Context, refund Refund, providerPaymentID *string, recipientEmail string) error {
	providerReference := ""
	if providerPaymentID != nil {
		providerReference = *providerPaymentID
	}
	result, err := service.provider.InitiateRefund(ctx, payment.RefundRequest{
		Reference:                refund.ID,
		IdempotencyKey:           refund.IdempotencyKey,
		AmountMinor:              refund.AmountMinor,
		Currency:                 payment.Currency,
		ProviderPaymentReference: providerReference,
	})
	if err != nil {
		return fmt.Errorf("initiate refund: %w", err)
	}
	// settle the refund, the purchase, the ledger row, and the receipt in one tx.
	_, err = service.store.SettleRefund(ctx, refund.ID, result.ProviderReference, recipientEmail)
	return err
}

func normalizeFilter(pageSize *int32, cursor **string) {
	if *pageSize <= 0 {
		*pageSize = defaultListLimit
	}
	if *pageSize > maxListLimit {
		*pageSize = maxListLimit
	}
	if *cursor != nil && **cursor == "" {
		*cursor = nil
	}
}

// fetch one extra row to know if there's another page. a short page means the
// listing ended, so no cursor is returned.
func refundPage(pageSize int32, refunds []Refund) RefundPage {
	page := RefundPage{Refunds: refunds}
	if len(refunds) > int(pageSize) {
		page.Refunds = refunds[:pageSize]
		last := page.Refunds[len(page.Refunds)-1]
		page.NextCursor = last.ID
	}
	return page
}

func payoutPage(pageSize int32, payouts []Payout) PayoutPage {
	page := PayoutPage{Payouts: payouts}
	if len(payouts) > int(pageSize) {
		page.Payouts = payouts[:pageSize]
		last := page.Payouts[len(page.Payouts)-1]
		page.NextCursor = last.ID
	}
	return page
}
