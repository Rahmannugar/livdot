package crews

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rahmannugar/livdot/internal/infra/pagination"
	"github.com/google/uuid"
)

const (
	defaultListLimit = 20
	maxListLimit     = 100
	maxCrewNameRunes = 200
)

var (
	ErrNotFound     = errors.New("crew profile not found")
	ErrInvalidInput = errors.New("invalid crew profile input")
)

type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityUnavailable Availability = "unavailable"
)

// a crew org account as the crews domain sees it.
type Profile struct {
	AccountID    string
	Name         string
	List         json.RawMessage
	Availability Availability
	UpdatedAt    time.Time
}

// narrows the crew list a host browses for assignment.
type Filter struct {
	Name         string
	Availability Availability
	Cursor       *Cursor
	PageSize     int32
}

// keyset position of the last profile in a page: (crew_name, account_id).
type Cursor struct {
	Name      string
	AccountID string
}

// one keyset page of crew profiles.
type Page struct {
	Profiles   []Profile
	NextCursor string
}

// the fully resolved profile written by the store.
type UpdateProfile struct {
	AccountID    string
	Name         string
	List         json.RawMessage
	Availability Availability
}

// optional fields of a crew profile patch.
type UpdateInput struct {
	Name         *string
	List         json.RawMessage
	Availability *Availability
}

// persistence port owned by the crews domain.
type Store interface {
	Get(ctx context.Context, accountID string) (Profile, error)
	Update(ctx context.Context, update UpdateProfile) (Profile, error)
	List(ctx context.Context, filter Filter) ([]Profile, error)
}

type Service struct {
	store Store
}

func NewService(store Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("crews store is required")
	}
	return &Service{store: store}, nil
}

func (service *Service) Profile(ctx context.Context, accountID string) (Profile, error) {
	return service.store.Get(ctx, accountID)
}

func (service *Service) UpdateProfile(
	ctx context.Context,
	accountID string,
	input UpdateInput,
) (Profile, error) {
	current, err := service.store.Get(ctx, accountID)
	if err != nil {
		return Profile{}, err
	}

	update := UpdateProfile{
		AccountID:    accountID,
		Name:         current.Name,
		List:         current.List,
		Availability: current.Availability,
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if err := validateName(name); err != nil {
			return Profile{}, err
		}
		update.Name = name
	}
	if input.Availability != nil {
		if !input.Availability.Valid() {
			return Profile{}, ErrInvalidInput
		}
		update.Availability = *input.Availability
	}
	if input.List != nil {
		list, err := normalizeList(input.List)
		if err != nil {
			return Profile{}, err
		}
		update.List = list
	}

	if update.Name == current.Name &&
		update.Availability == current.Availability &&
		bytes.Equal(update.List, current.List) {
		return current, nil
	}
	return service.store.Update(ctx, update)
}

func (service *Service) List(ctx context.Context, filter Filter) (Page, error) {
	if filter.Name != "" && utf8.RuneCountInString(filter.Name) > maxCrewNameRunes {
		return Page{}, ErrInvalidInput
	}
	if filter.Availability != "" && !filter.Availability.Valid() {
		return Page{}, ErrInvalidInput
	}
	if filter.PageSize <= 0 {
		filter.PageSize = defaultListLimit
	}
	if filter.PageSize > maxListLimit {
		filter.PageSize = maxListLimit
	}

	// fetch one extra row to know if there's another page. a short page means
	// the listing ended, so no cursor is returned.
	fetchSize := filter.PageSize + 1
	profiles, err := service.store.List(ctx, Filter{
		Name:         filter.Name,
		Availability: filter.Availability,
		Cursor:       filter.Cursor,
		PageSize:     fetchSize,
	})
	if err != nil {
		return Page{}, err
	}
	page := Page{Profiles: profiles}
	if len(profiles) > int(filter.PageSize) {
		page.Profiles = profiles[:filter.PageSize]
		last := page.Profiles[len(page.Profiles)-1]
		page.NextCursor = encodeCursor(Cursor{Name: last.Name, AccountID: last.AccountID})
	}
	return page, nil
}

// reports whether a crew account can be assigned to an event.
func (service *Service) Exists(ctx context.Context, accountID string) (bool, error) {
	_, err := service.store.Get(ctx, accountID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (availability Availability) Valid() bool {
	return availability == AvailabilityAvailable || availability == AvailabilityUnavailable
}

func validateName(name string) error {
	if name == "" || utf8.RuneCountInString(name) > maxCrewNameRunes {
		return ErrInvalidInput
	}
	return nil
}

func normalizeList(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '[' || !json.Valid(trimmed) {
		return nil, ErrInvalidInput
	}
	return trimmed, nil
}

// parses an opaque cursor emitted by a previous crews page.
func DecodeCursor(raw string) (*Cursor, error) {
	name, accountID, err := pagination.Decode(raw)
	if err != nil || name == "" {
		return nil, ErrInvalidInput
	}
	if _, err := uuid.Parse(accountID); err != nil {
		return nil, ErrInvalidInput
	}
	return &Cursor{Name: name, AccountID: accountID}, nil
}

func encodeCursor(cursor Cursor) string {
	return pagination.Encode(cursor.Name, cursor.AccountID)
}
