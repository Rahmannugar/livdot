package ticketingrepo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Rahmannugar/livdot/internal/infra/database"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestTicketReservationAgainstPostgres exercises the reservation transaction
// against a real PostgreSQL. Set LIVDOT_TEST_DATABASE_URL to run it; otherwise
// it is skipped so unit-only environments stay green.
func TestTicketReservationAgainstPostgres(t *testing.T) {
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

	store := NewTicketStore(pool)

	t.Run("one purchase per user per event", func(t *testing.T) {
		eventID, hostID := seedEvent(t, ctx, pool, 10)
		userID := seedAccount(t, ctx, pool, "user")
		_ = hostID

		first, _ := reserve(t, ctx, store, eventID, userID)
		if first.Status != ticketing.PurchaseInitiated {
			t.Fatalf("status = %q, want initiated", first.Status)
		}
		if _, _, err := store.Reserve(ctx, reserveInput(eventID, userID)); !errors.Is(err, ticketing.ErrAlreadyPurchased) {
			t.Fatalf("second reserve error = %v, want ErrAlreadyPurchased", err)
		}
	})

	t.Run("sold out returns no ticket", func(t *testing.T) {
		eventID, _ := seedEvent(t, ctx, pool, 1)
		firstUser := seedAccount(t, ctx, pool, "user")
		secondUser := seedAccount(t, ctx, pool, "user")

		reserve(t, ctx, store, eventID, firstUser)
		if _, _, err := store.Reserve(ctx, reserveInput(eventID, secondUser)); !errors.Is(err, ticketing.ErrSoldOut) {
			t.Fatalf("reserve error = %v, want ErrSoldOut", err)
		}
	})

	t.Run("concurrent buyers cannot oversell", func(t *testing.T) {
		eventID, _ := seedEvent(t, ctx, pool, 1)
		buyers := []string{
			seedAccount(t, ctx, pool, "user"),
			seedAccount(t, ctx, pool, "user"),
		}

		var wait sync.WaitGroup
		results := make([]error, len(buyers))
		for index, buyer := range buyers {
			wait.Add(1)
			go func(index int, buyer string) {
				defer wait.Done()
				_, _, err := store.Reserve(ctx, reserveInput(eventID, buyer))
				results[index] = err
			}(index, buyer)
		}
		wait.Wait()

		succeeded, soldOut := 0, 0
		for _, err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, ticketing.ErrSoldOut):
				soldOut++
			default:
				t.Fatalf("unexpected reserve error: %v", err)
			}
		}
		if succeeded != 1 || soldOut != 1 {
			t.Fatalf("succeeded = %d, soldOut = %d, want 1 and 1", succeeded, soldOut)
		}
	})

	// a redelivered paid webhook must not issue a second ticket or membership.
	t.Run("duplicate payment settles once", func(t *testing.T) {
		eventID, _ := seedEvent(t, ctx, pool, 5)
		userID := seedAccount(t, ctx, pool, "user")
		purchase, _ := reserve(t, ctx, store, eventID, userID)

		first, err := store.SettlePaid(ctx, ticketing.SettleInput{
			PurchaseID:        purchase.ID,
			ProviderPaymentID: "mock_chg_purchase",
			MemberID:          uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("first SettlePaid() error = %v", err)
		}
		if first.Expired || first.Ticket == nil {
			t.Fatalf("first settlement = %+v, want an issued ticket", first)
		}

		second, err := store.SettlePaid(ctx, ticketing.SettleInput{
			PurchaseID:        purchase.ID,
			ProviderPaymentID: "mock_chg_purchase",
			MemberID:          uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("second SettlePaid() error = %v", err)
		}
		if second.Ticket == nil || second.Ticket.ID != first.Ticket.ID {
			t.Fatalf("second settlement issued a different ticket: %+v", second.Ticket)
		}
		if second.Purchase.Status != ticketing.PurchasePaid {
			t.Fatalf("purchase status = %q, want paid", second.Purchase.Status)
		}

		var members int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM event_members WHERE event_id = $1 AND user_id = $2`,
			eventID, userID,
		).Scan(&members); err != nil {
			t.Fatalf("count members: %v", err)
		}
		if members != 1 {
			t.Fatalf("members = %d, want 1", members)
		}
	})

	// a payment that settles after the reservation lapsed is refundable, not a
	// failure: the charge stands and the purchase stays paid.
	t.Run("late payment settles as paid and expired", func(t *testing.T) {
		eventID, _ := seedEvent(t, ctx, pool, 5)
		userID := seedAccount(t, ctx, pool, "user")
		expired := reserveInput(eventID, userID)
		expired.ReservedAt = time.Now().Add(-2 * time.Minute)
		expired.ExpiresAt = time.Now().Add(-time.Minute)
		purchase, _, err := store.Reserve(ctx, expired)
		if err != nil {
			t.Fatalf("Reserve() error = %v", err)
		}

		result, err := store.SettlePaid(ctx, ticketing.SettleInput{
			PurchaseID:        purchase.ID,
			ProviderPaymentID: "mock_chg_late",
			MemberID:          uuid.NewString(),
		})
		if err != nil {
			t.Fatalf("SettlePaid() error = %v", err)
		}
		if !result.Expired {
			t.Fatal("late payment should report Expired")
		}
		if result.Purchase.Status != ticketing.PurchasePaid {
			t.Fatalf("status = %q, want paid", result.Purchase.Status)
		}
	})
}

func reserve(
	t *testing.T,
	ctx context.Context,
	store *TicketStore,
	eventID, userID string,
) (ticketing.Purchase, ticketing.Ticket) {
	t.Helper()
	purchase, ticket, err := store.Reserve(ctx, reserveInput(eventID, userID))
	if err != nil {
		t.Fatalf("Reserve() error = %v", err)
	}
	return purchase, ticket
}

func reserveInput(eventID, userID string) ticketing.ReserveInput {
	now := time.Now()
	return ticketing.ReserveInput{
		PurchaseID:     uuid.NewString(),
		TicketID:       uuid.NewString(),
		EventID:        eventID,
		UserID:         userID,
		AmountMinor:    500000,
		Provider:       "paystack",
		IdempotencyKey: uuid.NewString(),
		ReservedAt:     now,
		ExpiresAt:      now.Add(10 * time.Minute),
	}
}

func seedAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountType string) string {
	t.Helper()
	accountID := uuid.NewString()
	email := accountID + "@example.test"
	if _, err := pool.Exec(ctx,
		`INSERT INTO authentication_accounts (id, email, password_hash, account_type)
		 VALUES ($1, $2, 'hash', $3)`,
		accountID, email, accountType,
	); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	table := "users"
	fullName := "Test User"
	if accountType == "host" {
		table = "hosts"
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO `+table+` (account_id, full_name) VALUES ($1, $2)`,
		accountID, fullName,
	); err != nil {
		t.Fatalf("seed profile: %v", err)
	}
	return accountID
}

func seedEvent(t *testing.T, ctx context.Context, pool *pgxpool.Pool, totalTickets int32) (string, string) {
	t.Helper()
	hostID := seedAccount(t, ctx, pool, "host")
	eventID := uuid.NewString()
	startsAt := time.Now().Add(time.Hour)
	if _, err := pool.Exec(ctx,
		`INSERT INTO events (id, host_id, name, amount_minor, duration_seconds,
			total_tickets, available_tickets, starts_at, ends_at)
		 VALUES ($1, $2, $3, 500000, 3600, $4, $4, $5, $6)`,
		eventID, hostID, "Integration Event", totalTickets, startsAt, startsAt.Add(time.Hour),
	); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return eventID, hostID
}
