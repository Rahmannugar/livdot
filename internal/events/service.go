package events

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Rahmannugar/livdot/internal/infra/pagination"
	"github.com/Rahmannugar/livdot/internal/validation"
	"github.com/google/uuid"
)

const (
	defaultListLimit  = 20
	maxListLimit      = 100
	maxEventNameRunes = 200
)

var (
	ErrNotFound            = errors.New("event not found")
	ErrForbidden           = errors.New("event is owned by another host")
	ErrNotUpdatable        = errors.New("only upcoming events can be updated")
	ErrNotCancellable      = errors.New("only upcoming events can be cancelled")
	ErrTicketCountConflict = errors.New("tickets already reserved exceed the requested total")
	ErrConflict            = errors.New("event changed concurrently")
	ErrInvalidInput        = errors.New("invalid event input")
)

type Status string

const (
	StatusUpcoming  Status = "upcoming"
	StatusLive      Status = "live"
	StatusEnded     Status = "ended"
	StatusCancelled Status = "cancelled"
)

// the crew assignment as the events domain sees it.
type CrewSummary struct {
	AccountID    string
	Name         string
	Availability string
}

type Event struct {
	ID               string
	HostID           string
	AssignedCrewID   *string
	AssignedCrew     *CrewSummary
	Name             string
	AmountMinor      int64
	DurationSeconds  int32
	Status           Status
	TotalTickets     int32
	AvailableTickets int32
	StartsAt         time.Time
	EndsAt           time.Time
	CancelledAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Filter struct {
	Name        *string
	Status      *Status
	DurationGte *int32
	DurationLte *int32
	AmountGte   *int64
	AmountLte   *int64
	Cursor      *Cursor
	PageSize    int32
}

// keyset position of the last event in a page: (starts_at, id).
type Cursor struct {
	StartsAt time.Time
	ID       string
}

// one keyset page of events.
type Page struct {
	Events     []Event
	NextCursor string
}

// the fully resolved event written when a host creates one.
type NewEvent struct {
	HostID          string
	AssignedCrewID  *string
	Name            string
	AmountMinor     int64
	DurationSeconds int32
	TotalTickets    int32
	StartsAt        time.Time
	EndsAt          time.Time
}

// the fully resolved event written when a host updates one.
type EventUpdate struct {
	ID              string
	Name            string
	AmountMinor     int64
	DurationSeconds int32
	TotalTickets    int32
	AssignedCrewID  *string
	StartsAt        time.Time
	EndsAt          time.Time
}

type CreateInput struct {
	Name            string
	AmountMinor     int64
	DurationSeconds int32
	TotalTickets    int32
	StartsAt        time.Time
	AssignedCrewID  *string
}

type UpdateInput struct {
	Name            *string
	AmountMinor     *int64
	DurationSeconds *int32
	TotalTickets    *int32
	StartsAt        *time.Time
	AssignedCrewSet bool
	AssignedCrewID  *string
	Cancel          bool
}

// persistence port owned by the events domain.
type Store interface {
	Create(ctx context.Context, event NewEvent, recipientEmail string) (Event, error)
	Get(ctx context.Context, id string) (Event, error)
	Detail(ctx context.Context, id string) (Event, error)
	Update(ctx context.Context, update EventUpdate) (Event, error)
	Cancel(ctx context.Context, id string) (Event, error)
	List(ctx context.Context, filter Filter) ([]Event, error)
}

// lets events validate a crew assignment without touching crews storage.
type CrewDirectory interface {
	Exists(ctx context.Context, accountID string) (bool, error)
}

// reports whether an account holds paid access to an event. The ticketing domain
// owns entitlement, so events asks it instead of reading its tables.
type Membership interface {
	HasAccess(ctx context.Context, eventID, accountID string) (bool, error)
}

// resolves a recipient email so the crew notice can be queued in-transaction.
type Directory interface {
	AccountEmail(ctx context.Context, accountID string) (string, error)
}

type Service struct {
	store      Store
	crews      CrewDirectory
	directory  Directory
	membership Membership
	now        func() time.Time
}

func NewService(store Store, crews CrewDirectory, directory Directory, membership Membership) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("events store is required")
	}
	if crews == nil {
		return nil, fmt.Errorf("crew directory is required")
	}
	if directory == nil {
		return nil, fmt.Errorf("recipient directory is required")
	}
	if membership == nil {
		return nil, fmt.Errorf("membership resolver is required")
	}
	return &Service{
		store:      store,
		crews:      crews,
		directory:  directory,
		membership: membership,
		now:        time.Now,
	}, nil
}

func (service *Service) Create(ctx context.Context, hostID string, input CreateInput) (Event, error) {
	name := strings.TrimSpace(input.Name)
	if err := validateSchedule(name, input.AmountMinor, input.DurationSeconds, input.TotalTickets, input.StartsAt, service.now()); err != nil {
		return Event{}, err
	}
	recipient := ""
	if input.AssignedCrewID != nil {
		if err := service.ensureCrew(ctx, *input.AssignedCrewID); err != nil {
			return Event{}, err
		}
		// best effort: a missing address must not block event creation.
		if email, err := service.directory.AccountEmail(ctx, *input.AssignedCrewID); err == nil {
			recipient = email
		}
	}
	return service.store.Create(ctx, NewEvent{
		HostID:          hostID,
		AssignedCrewID:  input.AssignedCrewID,
		Name:            name,
		AmountMinor:     input.AmountMinor,
		DurationSeconds: input.DurationSeconds,
		TotalTickets:    input.TotalTickets,
		StartsAt:        input.StartsAt,
		EndsAt:          endsAt(input.StartsAt, input.DurationSeconds),
	}, recipient)
}

func (service *Service) List(ctx context.Context, filter Filter) (Page, error) {
	if filter.Status != nil && !filter.Status.Valid() {
		return Page{}, validation.New(ErrInvalidInput, "status",
			"status must be upcoming, live, ended, or cancelled")
	}
	if filter.DurationGte != nil && *filter.DurationGte <= 0 {
		return Page{}, validation.New(ErrInvalidInput, "duration[gte]",
			"duration[gte] must be greater than zero")
	}
	if filter.DurationLte != nil && *filter.DurationLte <= 0 {
		return Page{}, validation.New(ErrInvalidInput, "duration[lte]",
			"duration[lte] must be greater than zero")
	}
	if filter.AmountGte != nil && *filter.AmountGte < 0 {
		return Page{}, validation.New(ErrInvalidInput, "amount[gte]",
			"amount[gte] must be zero or greater")
	}
	if filter.AmountLte != nil && *filter.AmountLte < 0 {
		return Page{}, validation.New(ErrInvalidInput, "amount[lte]",
			"amount[lte] must be zero or greater")
	}
	if filter.DurationGte != nil && filter.DurationLte != nil && *filter.DurationGte > *filter.DurationLte {
		return Page{}, validation.New(ErrInvalidInput, "duration",
			"duration[gte] must not exceed duration[lte]")
	}
	if filter.AmountGte != nil && filter.AmountLte != nil && *filter.AmountGte > *filter.AmountLte {
		return Page{}, validation.New(ErrInvalidInput, "amount",
			"amount[gte] must not exceed amount[lte]")
	}
	if filter.Name != nil && utf8.RuneCountInString(*filter.Name) > maxEventNameRunes {
		return Page{}, validation.New(ErrInvalidInput, "name",
			"name must be 200 characters or fewer")
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
	events, err := service.store.List(ctx, Filter{
		Name:        filter.Name,
		Status:      filter.Status,
		DurationGte: filter.DurationGte,
		DurationLte: filter.DurationLte,
		AmountGte:   filter.AmountGte,
		AmountLte:   filter.AmountLte,
		Cursor:      filter.Cursor,
		PageSize:    fetchSize,
	})
	if err != nil {
		return Page{}, err
	}
	page := Page{Events: events}
	if len(events) > int(filter.PageSize) {
		page.Events = events[:filter.PageSize]
		last := page.Events[len(page.Events)-1]
		page.NextCursor = encodeCursor(Cursor{StartsAt: last.StartsAt, ID: last.ID})
	}
	return page, nil
}

func (service *Service) Detail(ctx context.Context, id string) (Event, error) {
	return service.store.Detail(ctx, id)
}

// Access reports whether the account holds paid access to the event.
func (service *Service) Access(ctx context.Context, eventID, accountID string) (bool, error) {
	return service.membership.HasAccess(ctx, eventID, accountID)
}

func (service *Service) Update(ctx context.Context, hostID, id string, input UpdateInput) (Event, error) {
	current, err := service.store.Get(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if current.HostID != hostID {
		return Event{}, ErrForbidden
	}
	if input.Cancel {
		if current.Status != StatusUpcoming {
			return Event{}, ErrNotCancellable
		}
		return service.store.Cancel(ctx, id)
	}
	if current.Status != StatusUpcoming {
		return Event{}, ErrNotUpdatable
	}

	name := current.Name
	if input.Name != nil {
		name = strings.TrimSpace(*input.Name)
	}
	amountMinor := current.AmountMinor
	if input.AmountMinor != nil {
		amountMinor = *input.AmountMinor
	}
	durationSeconds := current.DurationSeconds
	if input.DurationSeconds != nil {
		durationSeconds = *input.DurationSeconds
	}
	totalTickets := current.TotalTickets
	if input.TotalTickets != nil {
		totalTickets = *input.TotalTickets
	}
	startsAt := current.StartsAt
	if input.StartsAt != nil {
		startsAt = *input.StartsAt
	}
	assignedCrewID := current.AssignedCrewID
	if input.AssignedCrewSet {
		assignedCrewID = input.AssignedCrewID
	}

	if err := validateSchedule(name, amountMinor, durationSeconds, totalTickets, startsAt, service.now()); err != nil {
		return Event{}, err
	}
	if reserved := current.TotalTickets - current.AvailableTickets; totalTickets < reserved {
		return Event{}, ErrTicketCountConflict
	}
	if input.AssignedCrewSet && assignedCrewID != nil {
		if err := service.ensureCrew(ctx, *assignedCrewID); err != nil {
			return Event{}, err
		}
	}

	return service.store.Update(ctx, EventUpdate{
		ID:              id,
		Name:            name,
		AmountMinor:     amountMinor,
		DurationSeconds: durationSeconds,
		TotalTickets:    totalTickets,
		AssignedCrewID:  assignedCrewID,
		StartsAt:        startsAt,
		EndsAt:          endsAt(startsAt, durationSeconds),
	})
}

func (service *Service) ensureCrew(ctx context.Context, crewID string) error {
	exists, err := service.crews.Exists(ctx, crewID)
	if err != nil {
		return fmt.Errorf("check assigned crew: %w", err)
	}
	if !exists {
		return validation.New(ErrInvalidInput, "assignedCrewId",
			"assignedCrewId must identify an existing crew account")
	}
	return nil
}

func (status Status) Valid() bool {
	switch status {
	case StatusUpcoming, StatusLive, StatusEnded, StatusCancelled:
		return true
	default:
		return false
	}
}

func validateSchedule(
	name string,
	amountMinor int64,
	durationSeconds int32,
	totalTickets int32,
	startsAt time.Time,
	now time.Time,
) error {
	if name == "" || utf8.RuneCountInString(name) > maxEventNameRunes {
		if name == "" {
			return validation.New(ErrInvalidInput, "name", "name is required")
		}
		return validation.New(ErrInvalidInput, "name", "name must be 200 characters or fewer")
	}
	if amountMinor < 0 {
		return validation.New(ErrInvalidInput, "amountMinor", "amountMinor must be zero or greater")
	}
	if durationSeconds <= 0 {
		return validation.New(ErrInvalidInput, "durationSeconds", "durationSeconds must be greater than zero")
	}
	if totalTickets <= 0 {
		return validation.New(ErrInvalidInput, "totalTickets", "totalTickets must be greater than zero")
	}
	if startsAt.IsZero() || !startsAt.After(now) {
		return validation.New(ErrInvalidInput, "startsAt", "startsAt must be a future date and time")
	}
	return nil
}

func endsAt(startsAt time.Time, durationSeconds int32) time.Time {
	return startsAt.Add(time.Duration(durationSeconds) * time.Second)
}

// parses an opaque cursor emitted by a previous events page.
func DecodeCursor(raw string) (*Cursor, error) {
	sortKey, id, err := pagination.Decode(raw)
	if err != nil {
		return nil, validation.New(ErrInvalidInput, "cursor", "cursor is invalid or expired")
	}
	startsAt, err := time.Parse(time.RFC3339Nano, sortKey)
	if err != nil {
		return nil, validation.New(ErrInvalidInput, "cursor", "cursor is invalid or expired")
	}
	if _, err := uuid.Parse(id); err != nil {
		return nil, validation.New(ErrInvalidInput, "cursor", "cursor is invalid or expired")
	}
	return &Cursor{StartsAt: startsAt, ID: id}, nil
}

func encodeCursor(cursor Cursor) string {
	return pagination.Encode(cursor.StartsAt.UTC().Format(time.RFC3339Nano), cursor.ID)
}
