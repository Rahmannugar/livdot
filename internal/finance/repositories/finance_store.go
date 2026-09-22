package financerepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/finance"
	financedb "github.com/Rahmannugar/livdot/internal/finance/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

// SettleRefund marks the refund and its purchase refunded and records the ledger
// movement in one transaction.
func (store *FinanceStore) SettleRefund(ctx context.Context, refundID, providerRefundID string) (finance.Refund, error) {
	id, err := uuid.Parse(refundID)
	if err != nil {
		return finance.Refund{}, finance.ErrNotFound
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return finance.Refund{}, fmt.Errorf("begin refund settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := financedb.New(tx)
	record, err := queries.MarkRefunded(ctx, financedb.MarkRefundedParams{
		ID:               id,
		ProviderRefundID: &providerRefundID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Refund{}, fmt.Errorf("mark refunded: %w", err)
	}
	if _, err := queries.MarkPurchaseRefunded(ctx, record.PurchaseID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, fmt.Errorf("mark purchase refunded: %w", err)
	}
	entryID, err := uuid.NewV7()
	if err != nil {
		return finance.Refund{}, fmt.Errorf("generate ledger id: %w", err)
	}
	if _, err := queries.CreateLedgerEntry(ctx, financedb.CreateLedgerEntryParams{
		ID:          entryID,
		EventID:     record.EventID,
		EntryType:   financedb.LedgerEntryTypeRefund,
		AmountMinor: record.AmountMinor,
		RefundID:    pgtype.UUID{Bytes: record.ID, Valid: true},
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, fmt.Errorf("create ledger entry: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return finance.Refund{}, fmt.Errorf("commit refund settlement: %w", err)
	}
	return refundFromRecord(record), nil
}

func (store *FinanceStore) PendingRefundNotices(ctx context.Context, limit int32) ([]finance.RefundNotice, error) {
	records, err := financedb.New(store.pool).ListUnnotifiedRefunds(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list unnotified refunds: %w", err)
	}
	notices := make([]finance.RefundNotice, 0, len(records))
	for _, record := range records {
		notices = append(notices, finance.RefundNotice{
			RefundID:    record.ID.String(),
			UserID:      record.UserID.String(),
			EventID:     record.EventID.String(),
			AmountMinor: record.AmountMinor,
		})
	}
	return notices, nil
}

func (store *FinanceStore) MarkRefundNotified(ctx context.Context, refundID string) error {
	id, err := uuid.Parse(refundID)
	if err != nil {
		return finance.ErrInvalidInput
	}
	if _, err := financedb.New(store.pool).MarkRefundNotified(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("mark refund notified: %w", err)
	}
	return nil
}

func (store *FinanceStore) PaidPurchasesForEvent(ctx context.Context, eventID string) ([]finance.PurchaseForRefund, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return nil, finance.ErrInvalidInput
	}
	records, err := financedb.New(store.pool).ListPaidPurchasesForEvent(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list paid purchases: %w", err)
	}
	purchases := make([]finance.PurchaseForRefund, 0, len(records))
	for _, record := range records {
		purchases = append(purchases, finance.PurchaseForRefund{
			PurchaseID:        record.ID.String(),
			UserID:            record.UserID.String(),
			AmountMinor:       record.AmountMinor,
			Provider:          record.Provider,
			ProviderPaymentID: record.ProviderPaymentID,
		})
	}
	return purchases, nil
}

func (store *FinanceStore) SumPaidPurchases(ctx context.Context, eventID string) (int64, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return 0, finance.ErrInvalidInput
	}
	total, err := financedb.New(store.pool).SumPaidPurchases(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("sum paid purchases: %w", err)
	}
	return total, nil
}

func (store *FinanceStore) SumRefundedPurchases(ctx context.Context, eventID string) (int64, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return 0, finance.ErrInvalidInput
	}
	total, err := financedb.New(store.pool).SumRefundedPurchases(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("sum refunded purchases: %w", err)
	}
	return total, nil
}

func (store *FinanceStore) StreamStatus(ctx context.Context, eventID string) (string, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return "", finance.ErrNotFound
	}
	status, err := financedb.New(store.pool).GetEventStreamStatus(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", finance.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get event stream status: %w", err)
	}
	return string(status), nil
}

func (store *FinanceStore) EventForFinance(ctx context.Context, eventID string) (finance.Event, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return finance.Event{}, finance.ErrNotFound
	}
	record, err := financedb.New(store.pool).GetEventForFinance(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Event{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Event{}, fmt.Errorf("get event for finance: %w", err)
	}
	return finance.Event{
		ID:     record.ID.String(),
		HostID: record.HostID.String(),
		Status: string(record.Status),
	}, nil
}

func (store *FinanceStore) RefundByID(ctx context.Context, id string) (finance.Refund, error) {
	refundID, err := uuid.Parse(id)
	if err != nil {
		return finance.Refund{}, finance.ErrNotFound
	}
	record, err := financedb.New(store.pool).GetRefundByID(ctx, refundID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Refund{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Refund{}, fmt.Errorf("get refund: %w", err)
	}
	return refundFromRecord(record), nil
}

func (store *FinanceStore) Refunds(ctx context.Context, filter finance.RefundFilter) ([]finance.Refund, error) {
	params, err := refundListParams(filter)
	if err != nil {
		return nil, err
	}
	records, err := financedb.New(store.pool).ListRefunds(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list refunds: %w", err)
	}
	refunds := make([]finance.Refund, 0, len(records))
	for _, record := range records {
		refunds = append(refunds, refundFromRecord(record))
	}
	return refunds, nil
}

// CreatePayout opens a payout once per event. created=false means one already
// exists, so the caller must not pay again.
func (store *FinanceStore) CreatePayout(ctx context.Context, input finance.PayoutInput) (finance.Payout, bool, error) {
	payoutID, err := uuid.NewV7()
	if err != nil {
		return finance.Payout{}, false, fmt.Errorf("generate payout id: %w", err)
	}
	eventID, err := uuid.Parse(input.EventID)
	if err != nil {
		return finance.Payout{}, false, finance.ErrInvalidInput
	}
	hostID, err := uuid.Parse(input.HostID)
	if err != nil {
		return finance.Payout{}, false, finance.ErrInvalidInput
	}
	record, err := financedb.New(store.pool).CreateEventPayout(ctx, financedb.CreateEventPayoutParams{
		ID:             payoutID,
		EventID:        eventID,
		HostID:         hostID,
		AmountMinor:    input.AmountMinor,
		Provider:       input.Provider,
		IdempotencyKey: input.IdempotencyKey,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Payout{}, false, nil
	}
	if err != nil {
		return finance.Payout{}, false, fmt.Errorf("create payout: %w", err)
	}
	return payoutFromRecord(record), true, nil
}

func (store *FinanceStore) MarkPayoutPaid(ctx context.Context, payoutID, providerPayoutID string) (finance.Payout, error) {
	id, err := uuid.Parse(payoutID)
	if err != nil {
		return finance.Payout{}, finance.ErrNotFound
	}
	record, err := financedb.New(store.pool).MarkPayoutPaid(ctx, financedb.MarkPayoutPaidParams{
		ID:               id,
		ProviderPayoutID: &providerPayoutID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Payout{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Payout{}, fmt.Errorf("mark payout paid: %w", err)
	}
	return payoutFromRecord(record), nil
}

func (store *FinanceStore) PayoutByID(ctx context.Context, id string) (finance.Payout, error) {
	payoutID, err := uuid.Parse(id)
	if err != nil {
		return finance.Payout{}, finance.ErrNotFound
	}
	record, err := financedb.New(store.pool).GetPayoutByID(ctx, payoutID)
	if errors.Is(err, pgx.ErrNoRows) {
		return finance.Payout{}, finance.ErrNotFound
	}
	if err != nil {
		return finance.Payout{}, fmt.Errorf("get payout: %w", err)
	}
	return payoutFromRecord(record), nil
}

func (store *FinanceStore) Payouts(ctx context.Context, filter finance.PayoutFilter) ([]finance.Payout, error) {
	params, err := payoutListParams(filter)
	if err != nil {
		return nil, err
	}
	records, err := financedb.New(store.pool).ListPayouts(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("list payouts: %w", err)
	}
	payouts := make([]finance.Payout, 0, len(records))
	for _, record := range records {
		payouts = append(payouts, payoutFromRecord(record))
	}
	return payouts, nil
}

// RecordLedger appends a movement. Ledger rows are unique per referenced refund
// or payout, so a retry that already recorded is silently skipped.
func (store *FinanceStore) RecordLedger(ctx context.Context, input finance.LedgerInput) error {
	entryID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate ledger id: %w", err)
	}
	eventID, err := uuid.Parse(input.EventID)
	if err != nil {
		return finance.ErrInvalidInput
	}
	if _, err := financedb.New(store.pool).CreateLedgerEntry(ctx, financedb.CreateLedgerEntryParams{
		ID:          entryID,
		EventID:     eventID,
		EntryType:   financedb.LedgerEntryType(input.EntryType),
		AmountMinor: input.AmountMinor,
		PurchaseID:  optionalUUID(input.PurchaseID),
		RefundID:    optionalUUID(input.RefundID),
		PayoutID:    optionalUUID(input.PayoutID),
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("create ledger entry: %w", err)
	}
	return nil
}

func refundListParams(filter finance.RefundFilter) (financedb.ListRefundsParams, error) {
	eventID, err := optionalUUIDValue(filter.EventID)
	if err != nil {
		return financedb.ListRefundsParams{}, err
	}
	userID, err := optionalUUIDValue(filter.UserID)
	if err != nil {
		return financedb.ListRefundsParams{}, err
	}
	cursor, err := optionalUUIDValue(filter.Cursor)
	if err != nil {
		return financedb.ListRefundsParams{}, err
	}
	return financedb.ListRefundsParams{
		EventID:  eventID,
		UserID:   userID,
		Cursor:   cursor,
		PageSize: filter.PageSize,
	}, nil
}

func payoutListParams(filter finance.PayoutFilter) (financedb.ListPayoutsParams, error) {
	eventID, err := optionalUUIDValue(filter.EventID)
	if err != nil {
		return financedb.ListPayoutsParams{}, err
	}
	hostID, err := optionalUUIDValue(filter.HostID)
	if err != nil {
		return financedb.ListPayoutsParams{}, err
	}
	cursor, err := optionalUUIDValue(filter.Cursor)
	if err != nil {
		return financedb.ListPayoutsParams{}, err
	}
	return financedb.ListPayoutsParams{
		EventID:  eventID,
		HostID:   hostID,
		Cursor:   cursor,
		PageSize: filter.PageSize,
	}, nil
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
		RefundedAt:       optionalTime(record.RefundedAt),
	}
}

func payoutFromRecord(record financedb.EventPayout) finance.Payout {
	return finance.Payout{
		ID:               record.ID.String(),
		EventID:          record.EventID.String(),
		HostID:           record.HostID.String(),
		AmountMinor:      record.AmountMinor,
		Status:           string(record.Status),
		Provider:         record.Provider,
		ProviderPayoutID: record.ProviderPayoutID,
		IdempotencyKey:   record.IdempotencyKey,
		PaidAt:           optionalTime(record.PaidAt),
	}
}

func optionalUUID(value *string) pgtype.UUID {
	if value == nil || *value == "" {
		return pgtype.UUID{}
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: id, Valid: true}
}

func optionalUUIDValue(value *string) (pgtype.UUID, error) {
	if value == nil || *value == "" {
		return pgtype.UUID{}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return pgtype.UUID{}, finance.ErrInvalidInput
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
