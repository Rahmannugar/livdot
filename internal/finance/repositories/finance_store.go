package financerepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/livdot/internal/finance"
	financedb "github.com/Rahmannugar/livdot/internal/finance/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FinanceStore struct {
	pool *pgxpool.Pool
}

func NewFinanceStore(pool *pgxpool.Pool) *FinanceStore {
	return &FinanceStore{pool: pool}
}

// CreateRefund opens a refund once per purchase. A second call reports
// created=false so the caller does not move money twice.
func (store *FinanceStore) CreateRefund(ctx context.Context, input finance.RefundInput) (finance.Refund, bool, error) {
	refundID, err := uuid.NewV7()
	if err != nil {
		return finance.Refund{}, false, fmt.Errorf("generate refund id: %w", err)
	}
	eventID, err := uuid.Parse(input.EventID)
	if err != nil {
		return finance.Refund{}, false, finance.ErrInvalidInput
	}
	userID, err := uuid.Parse(input.UserID)
	if err != nil {
		return finance.Refund{}, false, finance.ErrInvalidInput
	}
	purchaseID, err := uuid.Parse(input.PurchaseID)
	if err != nil {
		return finance.Refund{}, false, finance.ErrInvalidInput
	}

	record, err := financedb.New(store.pool).CreateEventRefund(ctx, financedb.CreateEventRefundParams{
		ID:             refundID,
		EventID:        eventID,
		UserID:         userID,
		PurchaseID:     purchaseID,
		AmountMinor:    input.AmountMinor,
		Provider:       input.Provider,
		IdempotencyKey: input.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, false, nil
	}
	if err != nil {
		return finance.Refund{}, false, fmt.Errorf("create refund: %w", err)
	}
	return refundFromRecord(record), true, nil
}

func (store *FinanceStore) MarkRefunded(ctx context.Context, refundID, providerRefundID string) (finance.Refund, error) {
	id, err := uuid.Parse(refundID)
	if err != nil {
		return finance.Refund{}, finance.ErrNotFound
	}
	record, err := financedb.New(store.pool).MarkRefunded(ctx, financedb.MarkRefundedParams{
		ID:               id,
		ProviderRefundID: &providerRefundID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Refund{}, fmt.Errorf("mark refunded: %w", err)
	}
	return refundFromRecord(record), nil
}

func refundFromRecord(record financedb.EventRefund) finance.Refund {
	return finance.Refund{
		ID:               record.ID.String(),
		EventID:          record.EventID.String(),
		UserID:           record.UserID.String(),
		PurchaseID:       record.PurchaseID.String(),
		AmountMinor:      record.AmountMinor,
		Status:           string(record.Status),
		Provider:         record.Provider,
		ProviderRefundID: record.ProviderRefundID,
		IdempotencyKey:   record.IdempotencyKey,
	}
}
