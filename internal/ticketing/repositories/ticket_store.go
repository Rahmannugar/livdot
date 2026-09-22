package ticketingrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/ticketing"
	ticketingdb "github.com/Rahmannugar/livdot/internal/ticketing/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TicketStore struct {
	pool *pgxpool.Pool
}

func NewTicketStore(pool *pgxpool.Pool) *TicketStore {
	return &TicketStore{pool: pool}
}

func (store *TicketStore) EventForReservation(ctx context.Context, eventID string) (ticketing.ReservableEvent, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return ticketing.ReservableEvent{}, ticketing.ErrNotFound
	}
	record, err := ticketingdb.New(store.pool).GetEventForReservation(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketing.ReservableEvent{}, ticketing.ErrNotFound
	}
	if err != nil {
		return ticketing.ReservableEvent{}, fmt.Errorf("get event for reservation: %w", err)
	}
	return ticketing.ReservableEvent{
		ID:               record.ID.String(),
		Status:           string(record.Status),
		AmountMinor:      record.AmountMinor,
		AvailableTickets: record.AvailableTickets,
		StartsAt:         record.StartsAt.Time,
	}, nil
}

func (store *TicketStore) FindPurchaseByIdempotencyKey(ctx context.Context, userID, key string) (ticketing.Purchase, error) {
	id, err := uuid.Parse(userID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrInvalidInput
	}
	record, err := ticketingdb.New(store.pool).GetPurchaseByIdempotencyKey(ctx, ticketingdb.GetPurchaseByIdempotencyKeyParams{
		UserID:         id,
		IdempotencyKey: key,
	})
	return purchase(record, err)
}

func (store *TicketStore) FindPurchaseByEventAndUser(ctx context.Context, eventID, userID string) (ticketing.Purchase, error) {
	event, err := uuid.Parse(eventID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrInvalidInput
	}
	user, err := uuid.Parse(userID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrInvalidInput
	}
	record, err := ticketingdb.New(store.pool).GetPurchaseByEventAndUser(ctx, ticketingdb.GetPurchaseByEventAndUserParams{
		EventID: event,
		UserID:  user,
	})
	return purchase(record, err)
}

func (store *TicketStore) FindPurchaseByID(ctx context.Context, id string) (ticketing.Purchase, error) {
	purchaseID, err := uuid.Parse(id)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	record, err := ticketingdb.New(store.pool).GetPurchaseByID(ctx, purchaseID)
	return purchase(record, err)
}

// Reserve holds a slot and creates the payment intent + ticket in one tx. The
// guarded decrement is the concurrency guard: two buyers racing for the last
// slot cannot both succeed because the WHERE clause re-checks availability.
func (store *TicketStore) Reserve(ctx context.Context, input ticketing.ReserveInput) (ticketing.Purchase, ticketing.Ticket, error) {
	purchaseID, err := uuid.Parse(input.PurchaseID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrInvalidInput
	}
	ticketID, err := uuid.Parse(input.TicketID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrInvalidInput
	}
	eventID, err := uuid.Parse(input.EventID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrInvalidInput
	}
	userID, err := uuid.Parse(input.UserID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrInvalidInput
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, fmt.Errorf("begin reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := ticketingdb.New(tx)
	if _, err := queries.ReserveEventTicket(ctx, eventID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrSoldOut
		}
		return ticketing.Purchase{}, ticketing.Ticket{}, fmt.Errorf("reserve event ticket: %w", err)
	}
	purchaseRecord, err := queries.CreatePurchase(ctx, ticketingdb.CreatePurchaseParams{
		ID:             purchaseID,
		EventID:        eventID,
		UserID:         userID,
		AmountMinor:    input.AmountMinor,
		Provider:       input.Provider,
		IdempotencyKey: input.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ticketing.Purchase{}, ticketing.Ticket{}, ticketing.ErrAlreadyPurchased
		}
		return ticketing.Purchase{}, ticketing.Ticket{}, fmt.Errorf("create purchase: %w", err)
	}
	ticketRecord, err := queries.CreateTemporaryTicket(ctx, ticketingdb.CreateTemporaryTicketParams{
		ID:                   ticketID,
		EventID:              eventID,
		UserID:               userID,
		PurchaseID:           purchaseID,
		ReservedAt:           timestamp(input.ReservedAt),
		ReservationExpiresAt: timestamp(input.ExpiresAt),
	})
	if err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, fmt.Errorf("create temporary ticket: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ticketing.Purchase{}, ticketing.Ticket{}, fmt.Errorf("commit reservation: %w", err)
	}
	return purchaseFromRecord(purchaseRecord), ticketFromRecord(ticketRecord), nil
}

func (store *TicketStore) SetCheckoutURL(ctx context.Context, purchaseID, url string) (ticketing.Purchase, error) {
	id, err := uuid.Parse(purchaseID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	record, err := ticketingdb.New(store.pool).UpdatePurchaseCheckout(ctx, ticketingdb.UpdatePurchaseCheckoutParams{
		ID:          id,
		CheckoutUrl: &url,
	})
	return purchase(record, err)
}

func (store *TicketStore) MarkProcessing(ctx context.Context, purchaseID string) (ticketing.Purchase, error) {
	id, err := uuid.Parse(purchaseID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	record, err := ticketingdb.New(store.pool).MarkPurchaseProcessing(ctx, id)
	return purchase(record, err)
}

// SettlePaid issues the ticket and grants membership in one tx. If the ticket's
// reservation lapsed the update affects no row, so we mark the charge expired
// and let the caller refund rather than granting access.
func (store *TicketStore) SettlePaid(ctx context.Context, input ticketing.SettleInput) (ticketing.SettleResult, error) {
	purchaseID, err := uuid.Parse(input.PurchaseID)
	if err != nil {
		return ticketing.SettleResult{}, ticketing.ErrNotFound
	}
	memberID, err := uuid.Parse(input.MemberID)
	if err != nil {
		return ticketing.SettleResult{}, ticketing.ErrInvalidInput
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ticketing.SettleResult{}, fmt.Errorf("begin settlement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := ticketingdb.New(tx)
	purchaseRecord, err := queries.GetPurchaseByID(ctx, purchaseID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ticketing.SettleResult{}, ticketing.ErrNotFound
		}
		return ticketing.SettleResult{}, fmt.Errorf("get purchase: %w", err)
	}
	// already settled; a redelivered webhook must not re-issue a ticket.
	if purchaseRecord.Status == ticketingdb.PurchaseStatusPaid {
		ticketRecord, err := queries.GetTicketByPurchase(ctx, purchaseID)
		if err != nil {
			return ticketing.SettleResult{}, fmt.Errorf("get settled ticket: %w", err)
		}
		return ticketing.SettleResult{Purchase: purchaseFromRecord(purchaseRecord), Ticket: ticketPtr(ticketRecord)}, nil
	}
	if purchaseRecord.Status == ticketingdb.PurchaseStatusFailed ||
		purchaseRecord.Status == ticketingdb.PurchaseStatusRefunded {
		return ticketing.SettleResult{}, ticketing.ErrReservationGone
	}

	paidRecord, err := queries.MarkPurchasePaid(ctx, ticketingdb.MarkPurchasePaidParams{
		ID:                purchaseID,
		ProviderPaymentID: &input.ProviderPaymentID,
	})
	if err != nil {
		return ticketing.SettleResult{}, fmt.Errorf("mark purchase paid: %w", err)
	}
	ticketRecord, err := queries.IssueTicketByPurchase(ctx, purchaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		// reservation lapsed, so this is a late payment.
		if _, failErr := queries.MarkPurchaseFailed(ctx, purchaseID); failErr != nil {
			return ticketing.SettleResult{}, fmt.Errorf("mark late purchase failed: %w", failErr)
		}
		if err := tx.Commit(ctx); err != nil {
			return ticketing.SettleResult{}, fmt.Errorf("commit late payment: %w", err)
		}
		return ticketing.SettleResult{Purchase: purchaseFromRecord(paidRecord), Expired: true}, nil
	}
	if err != nil {
		return ticketing.SettleResult{}, fmt.Errorf("issue ticket: %w", err)
	}
	if _, err := queries.CreateEventMember(ctx, ticketingdb.CreateEventMemberParams{
		ID:       memberID,
		EventID:  purchaseRecord.EventID,
		UserID:   purchaseRecord.UserID,
		TicketID: ticketRecord.ID,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ticketing.SettleResult{}, fmt.Errorf("create event member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ticketing.SettleResult{}, fmt.Errorf("commit settlement: %w", err)
	}
	return ticketing.SettleResult{
		Purchase: purchaseFromRecord(paidRecord),
		Ticket:   ticketPtr(ticketRecord),
	}, nil
}

func (store *TicketStore) FailAndRelease(ctx context.Context, purchaseID string) (ticketing.Purchase, error) {
	id, err := uuid.Parse(purchaseID)
	if err != nil {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return ticketing.Purchase{}, fmt.Errorf("begin release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := ticketingdb.New(tx)
	current, err := queries.GetPurchaseByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	if err != nil {
		return ticketing.Purchase{}, fmt.Errorf("get purchase: %w", err)
	}
	// a failure webhook that arrives after success must not revoke access.
	if current.Status == ticketingdb.PurchaseStatusPaid ||
		current.Status == ticketingdb.PurchaseStatusRefunded {
		return purchaseFromRecord(current), nil
	}
	record, err := queries.MarkPurchaseFailed(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return purchaseFromRecord(current), nil
	}
	if err != nil {
		return ticketing.Purchase{}, fmt.Errorf("mark purchase failed: %w", err)
	}
	if _, err := queries.ExpireTicketByPurchase(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ticketing.Purchase{}, fmt.Errorf("expire ticket: %w", err)
	}
	if _, err := queries.ReleaseEventTicket(ctx, record.EventID); err != nil {
		return ticketing.Purchase{}, fmt.Errorf("release event ticket: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ticketing.Purchase{}, fmt.Errorf("commit release: %w", err)
	}
	return purchaseFromRecord(record), nil
}

func (store *TicketStore) TicketByID(ctx context.Context, id string) (ticketing.Ticket, error) {
	ticketID, err := uuid.Parse(id)
	if err != nil {
		return ticketing.Ticket{}, ticketing.ErrNotFound
	}
	record, err := ticketingdb.New(store.pool).GetTicketByID(ctx, ticketID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketing.Ticket{}, ticketing.ErrNotFound
	}
	if err != nil {
		return ticketing.Ticket{}, fmt.Errorf("get ticket: %w", err)
	}
	return ticketFromRecord(record), nil
}

// ExpireReservations claims lapsed reservations and returns each held slot to
// its event in one tx per ticket. FOR UPDATE SKIP LOCKED makes concurrent
// workers safe.
func (store *TicketStore) ExpireReservations(ctx context.Context, limit int32) (int, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin expiry: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := ticketingdb.New(tx)
	tickets, err := queries.ClaimExpiredTicketReservations(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("claim expired reservations: %w", err)
	}
	for _, ticket := range tickets {
		if _, err := queries.ReleaseEventTicket(ctx, ticket.EventID); err != nil {
			return 0, fmt.Errorf("release expired ticket: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit expiry: %w", err)
	}
	return len(tickets), nil
}

func purchase(record ticketingdb.EventPurchase, err error) (ticketing.Purchase, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return ticketing.Purchase{}, ticketing.ErrNotFound
	}
	if err != nil {
		return ticketing.Purchase{}, fmt.Errorf("get purchase: %w", err)
	}
	return purchaseFromRecord(record), nil
}

func purchaseFromRecord(record ticketingdb.EventPurchase) ticketing.Purchase {
	return ticketing.Purchase{
		ID:                record.ID.String(),
		EventID:           record.EventID.String(),
		UserID:            record.UserID.String(),
		AmountMinor:       record.AmountMinor,
		Status:            string(record.Status),
		Provider:          record.Provider,
		ProviderPaymentID: record.ProviderPaymentID,
		IdempotencyKey:    record.IdempotencyKey,
		CheckoutURL:       record.CheckoutUrl,
		PaidAt:            optionalTime(record.PaidAt),
		CreatedAt:         record.CreatedAt.Time,
	}
}

func ticketFromRecord(record ticketingdb.Ticket) ticketing.Ticket {
	return ticketing.Ticket{
		ID:                   record.ID.String(),
		EventID:              record.EventID.String(),
		UserID:               record.UserID.String(),
		Status:               string(record.Status),
		ReservationExpiresAt: record.ReservationExpiresAt.Time,
		IssuedAt:             optionalTime(record.IssuedAt),
	}
}

func ticketPtr(record ticketingdb.Ticket) *ticketing.Ticket {
	ticket := ticketFromRecord(record)
	return &ticket
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
