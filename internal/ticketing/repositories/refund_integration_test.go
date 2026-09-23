package ticketingrepo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	financerepo "github.com/Rahmannugar/livdot/internal/finance/repositories"
	"github.com/Rahmannugar/livdot/internal/infra/database"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestRefundRevokesAccessAgainstDatabase proves a refund revokes the ticket and
// membership, so `Accessible` stops reporting access and the buyer can purchase
// again. Set LIVDOT_TEST_DATABASE_URL to run it.
func TestRefundRevokesAccessAgainstDatabase(t *testing.T) {
	dsn := os.Getenv("LIVDOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIVDOT_TEST_DATABASE_URL to run the PostgreSQL integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	migrations, err := filepath.Abs(filepath.Join("..", "..", "infra", "database", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if err := database.Migrate(ctx, pool, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	eventID, _ := seedEvent(t, ctx, pool, 5)
	userID := seedAccount(t, ctx, pool, "user")
	purchase, ticket := seedIssuedPurchase(t, ctx, pool, eventID, userID)

	store := NewTicketStore(pool)
	active, err := store.ActiveEventIDs(ctx, userID, []string{eventID})
	if err != nil {
		t.Fatalf("ActiveEventIDs() error = %v", err)
	}
	if !active[eventID] {
		t.Fatal("a paid member should have access before the refund")
	}

	financeStore := financerepo.NewFinanceStore(pool)
	refundID := seedRefund(t, ctx, pool, eventID, userID, purchase)
	if _, err := financeStore.SettleRefund(ctx, refundID, "mock_rfnd_"+uuid.NewString(), ""); err != nil {
		t.Fatalf("SettleRefund() error = %v", err)
	}

	active, err = store.ActiveEventIDs(ctx, userID, []string{eventID})
	if err != nil {
		t.Fatalf("ActiveEventIDs() error = %v", err)
	}
	if active[eventID] {
		t.Fatal("a refunded purchase must not report access")
	}
	assertRevoked(t, ctx, pool, ticket)
}

// TestRefundedBuyerCanPurchaseAgain proves the refunded purchase no longer blocks
// a new attempt: it is reset in place rather than locked out.
func TestRefundedBuyerCanPurchaseAgain(t *testing.T) {
	dsn := os.Getenv("LIVDOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LIVDOT_TEST_DATABASE_URL to run the PostgreSQL integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()
	migrations, err := filepath.Abs(filepath.Join("..", "..", "infra", "database", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations path: %v", err)
	}
	if err := database.Migrate(ctx, pool, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	eventID, _ := seedEvent(t, ctx, pool, 5)
	userID := seedAccount(t, ctx, pool, "user")
	purchase, _ := seedIssuedPurchase(t, ctx, pool, eventID, userID)
	refundID := seedRefund(t, ctx, pool, eventID, userID, purchase)
	financeStore := financerepo.NewFinanceStore(pool)
	if _, err := financeStore.SettleRefund(ctx, refundID, "mock_rfnd_"+uuid.NewString(), ""); err != nil {
		t.Fatalf("SettleRefund() error = %v", err)
	}

	now := time.Now()
	store := NewTicketStore(pool)
	reset, err := store.ResetForRetry(ctx, ticketing.RetryInput{
		PurchaseID:     purchase,
		EventID:        eventID,
		UserID:         userID,
		IdempotencyKey: uuid.NewString(),
		ReservedAt:     now,
		ExpiresAt:      now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ResetForRetry() error = %v", err)
	}
	if reset.Status != ticketing.PurchaseInitiated {
		t.Fatalf("status = %q, want initiated", reset.Status)
	}
}

func seedIssuedPurchase(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, userID string) (string, string) {
	t.Helper()
	purchaseID := uuid.NewString()
	ticketID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO event_purchases (id, event_id, user_id, amount_minor, status, provider, idempotency_key, paid_at)
		 VALUES ($1, $2, $3, 500000, 'paid', 'paystack', $4, now())`,
		purchaseID, eventID, userID, uuid.NewString(),
	); err != nil {
		t.Fatalf("seed purchase: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tickets (id, event_id, user_id, purchase_id, status, reserved_at, reservation_expires_at, issued_at)
		 VALUES ($1, $2, $3, $4, 'issued', now(), now() + interval '10 minutes', now())`,
		ticketID, eventID, userID, purchaseID,
	); err != nil {
		t.Fatalf("seed ticket: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO event_members (id, event_id, user_id, ticket_id)
		 VALUES ($1, $2, $3, $4)`,
		uuid.NewString(), eventID, userID, ticketID,
	); err != nil {
		t.Fatalf("seed member: %v", err)
	}
	return purchaseID, ticketID
}

func seedRefund(t *testing.T, ctx context.Context, pool *pgxpool.Pool, eventID, userID, purchaseID string) string {
	t.Helper()
	refundID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO event_refunds (id, event_id, user_id, purchase_id, amount_minor, provider, idempotency_key)
		 VALUES ($1, $2, $3, $4, 500000, 'paystack', $5)`,
		refundID, eventID, userID, purchaseID, uuid.NewString(),
	); err != nil {
		t.Fatalf("seed refund: %v", err)
	}
	return refundID
}

func assertRevoked(t *testing.T, ctx context.Context, pool *pgxpool.Pool, ticketID string) {
	t.Helper()
	var ticketStatus, memberStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM tickets WHERE id = $1`, ticketID).Scan(&ticketStatus); err != nil {
		t.Fatalf("read ticket: %v", err)
	}
	if ticketStatus != "revoked" {
		t.Fatalf("ticket status = %q, want revoked", ticketStatus)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM event_members WHERE ticket_id = $1`, ticketID).Scan(&memberStatus); err != nil {
		t.Fatalf("read member: %v", err)
	}
	if memberStatus != "revoked" {
		t.Fatalf("member status = %q, want revoked", memberStatus)
	}
}
