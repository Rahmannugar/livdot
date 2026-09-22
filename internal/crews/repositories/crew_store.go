package crewsrepo

import (
	"context"
	"errors"
	"fmt"

	"github.com/Rahmannugar/livdot/internal/crews"
	crewsdb "github.com/Rahmannugar/livdot/internal/crews/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CrewStore struct {
	pool *pgxpool.Pool
}

func NewCrewStore(pool *pgxpool.Pool) *CrewStore {
	return &CrewStore{pool: pool}
}

func (store *CrewStore) Get(ctx context.Context, accountID string) (crews.Profile, error) {
	id, err := uuid.Parse(accountID)
	if err != nil {
		return crews.Profile{}, crews.ErrNotFound
	}
	crew, err := crewsdb.New(store.pool).GetCrewProfile(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return crews.Profile{}, crews.ErrNotFound
	}
	if err != nil {
		return crews.Profile{}, fmt.Errorf("get crew profile: %w", err)
	}
	return crewProfile(crew), nil
}

func (store *CrewStore) Update(
	ctx context.Context,
	update crews.UpdateProfile,
) (crews.Profile, error) {
	id, err := uuid.Parse(update.AccountID)
	if err != nil {
		return crews.Profile{}, crews.ErrNotFound
	}
	crew, err := crewsdb.New(store.pool).UpdateCrewProfile(ctx, crewsdb.UpdateCrewProfileParams{
		AccountID:          id,
		CrewName:           update.Name,
		CrewList:           []byte(update.List),
		AvailabilityStatus: crewsdb.CrewAvailabilityStatus(update.Availability),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return crews.Profile{}, crews.ErrNotFound
	}
	if err != nil {
		return crews.Profile{}, fmt.Errorf("update crew profile: %w", err)
	}
	return crewProfile(crew), nil
}

func (store *CrewStore) List(ctx context.Context, filter crews.Filter) ([]crews.Profile, error) {
	var name *string
	if filter.Name != "" {
		name = &filter.Name
	}
	var availability *crewsdb.CrewAvailabilityStatus
	if filter.Availability != "" {
		value := crewsdb.CrewAvailabilityStatus(filter.Availability)
		availability = &value
	}
	var cursorName *string
	var cursorID pgtype.UUID
	if filter.Cursor != nil {
		cursorName = &filter.Cursor.Name
		cursorID = pgtype.UUID{Bytes: uuid.MustParse(filter.Cursor.AccountID), Valid: true}
	}

	records, err := crewsdb.New(store.pool).ListCrews(ctx, crewsdb.ListCrewsParams{
		Name:           name,
		Availability:   availability,
		CursorCrewName: cursorName,
		CursorID:       cursorID,
		PageSize:       filter.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list crews: %w", err)
	}
	profiles := make([]crews.Profile, 0, len(records))
	for _, record := range records {
		profiles = append(profiles, crewProfile(record))
	}
	return profiles, nil
}

func crewProfile(crew crewsdb.Crew) crews.Profile {
	return crews.Profile{
		AccountID:    crew.AccountID.String(),
		Name:         crew.CrewName,
		List:         crew.CrewList,
		Availability: crews.Availability(crew.AvailabilityStatus),
		UpdatedAt:    crew.UpdatedAt.Time,
	}
}
