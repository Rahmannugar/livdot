package authenticationrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rahmannugar/authlier/emailpassword"
	authpassword "github.com/Rahmannugar/authlier/password"
	authenticationdb "github.com/Rahmannugar/livdot/internal/authentication/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type registrationDetailsKey struct{}

// carries product profile data through authlier's registration primitive without
// leaking it into that primitive's storage contract.
type RegistrationDetails struct {
	FullName string
	CrewName string
}

func WithRegistrationDetails(ctx context.Context, details RegistrationDetails) context.Context {
	return context.WithValue(ctx, registrationDetailsKey{}, details)
}

type AccountStore struct {
	pool        *pgxpool.Pool
	accountType authenticationdb.AuthenticationAccountType
}

func NewAccountStore(
	pool *pgxpool.Pool,
	accountType authenticationdb.AuthenticationAccountType,
) *AccountStore {
	return &AccountStore{pool: pool, accountType: accountType}
}

func (store *AccountStore) Register(
	ctx context.Context,
	registration emailpassword.Registration,
) (emailpassword.User, error) {
	details, ok := ctx.Value(registrationDetailsKey{}).(RegistrationDetails)
	if !ok {
		return emailpassword.User{}, fmt.Errorf("registration details are required")
	}
	if err := details.validate(store.accountType); err != nil {
		return emailpassword.User{}, err
	}

	accountID, err := uuid.NewV7()
	if err != nil {
		return emailpassword.User{}, fmt.Errorf("generate account ID: %w", err)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return emailpassword.User{}, fmt.Errorf("begin account registration: %w", err)
	}
	defer tx.Rollback(ctx)

	queries := authenticationdb.New(tx)
	if _, err := queries.CreateAccount(ctx, authenticationdb.CreateAccountParams{
		ID:           accountID,
		Email:        registration.Email,
		PasswordHash: registration.PasswordHash,
		AccountType:  store.accountType,
	}); err != nil {
		if isUniqueViolation(err) {
			return emailpassword.User{}, emailpassword.ErrConflict
		}
		return emailpassword.User{}, fmt.Errorf("create account: %w", err)
	}
	if err := createProfile(ctx, queries, store.accountType, accountID, details); err != nil {
		if isUniqueViolation(err) {
			return emailpassword.User{}, emailpassword.ErrConflict
		}
		return emailpassword.User{}, fmt.Errorf("create account profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return emailpassword.User{}, emailpassword.ErrConflict
		}
		return emailpassword.User{}, fmt.Errorf("commit account registration: %w", err)
	}

	return emailpassword.User{ID: accountID.String(), Email: registration.Email}, nil
}

func (store *AccountStore) FindByEmail(
	ctx context.Context,
	normalizedEmail string,
) (emailpassword.User, emailpassword.PasswordCredential, error) {
	account, err := authenticationdb.New(store.pool).FindAccountByEmail(ctx, normalizedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return emailpassword.User{}, emailpassword.PasswordCredential{}, emailpassword.ErrNotFound
	}
	if err != nil {
		return emailpassword.User{}, emailpassword.PasswordCredential{}, err
	}
	if account.AccountType != store.accountType {
		return emailpassword.User{}, emailpassword.PasswordCredential{}, emailpassword.ErrNotFound
	}
	return emailpassword.User{
			ID:    account.ID.String(),
			Email: account.Email,
		}, emailpassword.PasswordCredential{
			UserID:       account.ID.String(),
			PasswordHash: account.PasswordHash,
		}, nil
}

func (store *AccountStore) ReplacePasswordHash(
	ctx context.Context,
	userID string,
	currentHash string,
	replacementHash string,
	updatedAt time.Time,
) error {
	accountID, err := uuid.Parse(userID)
	if err != nil {
		return emailpassword.ErrNotFound
	}
	rows, err := authenticationdb.New(store.pool).ReplacePasswordHash(ctx, authenticationdb.ReplacePasswordHashParams{
		PasswordHash:   replacementHash,
		UpdatedAt:      pgtype.Timestamptz{Time: updatedAt, Valid: true},
		ID:             accountID,
		AccountType:    store.accountType,
		PasswordHash_2: currentHash,
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		return emailpassword.ErrConflict
	}
	return nil
}

func (store *AccountStore) FindAccountByID(ctx context.Context, accountID uuid.UUID) (authenticationdb.AuthenticationAccount, error) {
	return authenticationdb.New(store.pool).FindAccountByID(ctx, accountID)
}

// repository-only; no HTTP handler calls this.
func (store *AccountStore) CreateInternalAdmin(
	ctx context.Context,
	email string,
	passwordHash string,
	fullName string,
	role authenticationdb.InternalAdminRole,
) (authenticationdb.AuthenticationAccount, error) {
	accountID, err := uuid.NewV7()
	if err != nil {
		return authenticationdb.AuthenticationAccount{}, fmt.Errorf("generate account ID: %w", err)
	}

	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return authenticationdb.AuthenticationAccount{}, fmt.Errorf("begin internal admin creation: %w", err)
	}
	defer tx.Rollback(ctx)

	queries := authenticationdb.New(tx)
	account, err := queries.CreateAccount(ctx, authenticationdb.CreateAccountParams{
		ID:           accountID,
		Email:        email,
		PasswordHash: passwordHash,
		AccountType:  authenticationdb.AuthenticationAccountTypeInternalAdmin,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return authenticationdb.AuthenticationAccount{}, emailpassword.ErrConflict
		}
		return authenticationdb.AuthenticationAccount{}, fmt.Errorf("create internal admin account: %w", err)
	}
	if _, err := queries.CreateInternalAdminProfile(ctx, authenticationdb.CreateInternalAdminProfileParams{
		AccountID: accountID,
		FullName:  fullName,
		Role:      role,
	}); err != nil {
		if isUniqueViolation(err) {
			return authenticationdb.AuthenticationAccount{}, emailpassword.ErrConflict
		}
		return authenticationdb.AuthenticationAccount{}, fmt.Errorf("create internal admin profile: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		if isUniqueViolation(err) {
			return authenticationdb.AuthenticationAccount{}, emailpassword.ErrConflict
		}
		return authenticationdb.AuthenticationAccount{}, fmt.Errorf("commit internal admin creation: %w", err)
	}
	return account, nil
}

func createProfile(
	ctx context.Context,
	queries *authenticationdb.Queries,
	accountType authenticationdb.AuthenticationAccountType,
	accountID uuid.UUID,
	details RegistrationDetails,
) error {
	switch accountType {
	case authenticationdb.AuthenticationAccountTypeUser:
		_, err := queries.CreateUserProfile(ctx, authenticationdb.CreateUserProfileParams{
			AccountID: accountID,
			FullName:  details.FullName,
		})
		return err
	case authenticationdb.AuthenticationAccountTypeHost:
		_, err := queries.CreateHostProfile(ctx, authenticationdb.CreateHostProfileParams{
			AccountID: accountID,
			FullName:  details.FullName,
		})
		return err
	case authenticationdb.AuthenticationAccountTypeCrew:
		_, err := queries.CreateCrewProfile(ctx, authenticationdb.CreateCrewProfileParams{
			AccountID: accountID,
			CrewName:  details.CrewName,
		})
		return err
	default:
		return fmt.Errorf("unsupported public account type %q", accountType)
	}
}

func (details RegistrationDetails) validate(accountType authenticationdb.AuthenticationAccountType) error {
	switch accountType {
	case authenticationdb.AuthenticationAccountTypeUser, authenticationdb.AuthenticationAccountTypeHost:
		if strings.TrimSpace(details.FullName) == "" {
			return fmt.Errorf("full name is required")
		}
	case authenticationdb.AuthenticationAccountTypeCrew:
		if strings.TrimSpace(details.CrewName) == "" {
			return fmt.Errorf("crew name is required")
		}
	default:
		return fmt.Errorf("unsupported registration account type %q", accountType)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func HashPassword(plainPassword string) (string, error) {
	return authpassword.Hash(plainPassword)
}
