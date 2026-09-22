package authenticationrepo

import (
	"context"
	"errors"
	"fmt"

	authenticationdb "github.com/Rahmannugar/livdot/internal/authentication/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// no account for the requested subject.
var ErrAccountNotFound = errors.New("authentication account not found")

// resolves a session subject's account type. sessions only carry the subject
// id, so authorization needs this lookup.
type AccountDirectory struct {
	pool *pgxpool.Pool
}

func NewAccountDirectory(pool *pgxpool.Pool) *AccountDirectory {
	return &AccountDirectory{pool: pool}
}

func (directory *AccountDirectory) AccountType(
	ctx context.Context,
	accountID uuid.UUID,
) (authenticationdb.AuthenticationAccountType, error) {
	accountType, err := authenticationdb.New(directory.pool).FindAccountType(ctx, accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrAccountNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find account type: %w", err)
	}
	return accountType, nil
}
