package eventsrepo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Rahmannugar/livdot/internal/events"
	eventsdb "github.com/Rahmannugar/livdot/internal/events/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EventStore struct {
	pool *pgxpool.Pool
}

func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

func (store *EventStore) Create(ctx context.Context, event events.NewEvent) (events.Event, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return events.Event{}, fmt.Errorf("generate event ID: %w", err)
	}
	hostID, err := uuid.Parse(event.HostID)
	if err != nil {
		return events.Event{}, events.ErrInvalidInput
	}
	assignedCrewID, err := nullableUUID(event.AssignedCrewID)
	if err != nil {
		return events.Event{}, err
	}

	record, err := eventsdb.New(store.pool).CreateEvent(ctx, eventsdb.CreateEventParams{
		ID:              eventID,
		HostID:          hostID,
		AssignedCrewID:  assignedCrewID,
		Name:            event.Name,
		AmountMinor:     event.AmountMinor,
		DurationSeconds: event.DurationSeconds,
		TotalTickets:    event.TotalTickets,
		StartsAt:        timestamp(event.StartsAt),
		EndsAt:          timestamp(event.EndsAt),
	})
	if err != nil {
		return events.Event{}, fmt.Errorf("create event: %w", err)
	}
	return eventRecord(record), nil
}

func (store *EventStore) Get(ctx context.Context, id string) (events.Event, error) {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return events.Event{}, events.ErrNotFound
	}
	record, err := eventsdb.New(store.pool).GetEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, events.ErrNotFound
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("get event: %w", err)
	}
	return eventRecord(record), nil
}

func (store *EventStore) Detail(ctx context.Context, id string) (events.Event, error) {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return events.Event{}, events.ErrNotFound
	}
	record, err := eventsdb.New(store.pool).GetEventDetail(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, events.ErrNotFound
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("get event detail: %w", err)
	}

	event := eventDetail(record)
	if record.AssignedCrewID.Valid && record.CrewName != nil {
		event.AssignedCrew = &events.CrewSummary{
			AccountID:    uuid.UUID(record.AssignedCrewID.Bytes).String(),
			Name:         *record.CrewName,
			Availability: string(derefAvailability(record.AvailabilityStatus)),
		}
	}
	return event, nil
}

func (store *EventStore) Update(ctx context.Context, update events.EventUpdate) (events.Event, error) {
	eventID, err := uuid.Parse(update.ID)
	if err != nil {
		return events.Event{}, events.ErrNotFound
	}
	assignedCrewID, err := nullableUUID(update.AssignedCrewID)
	if err != nil {
		return events.Event{}, err
	}

	record, err := eventsdb.New(store.pool).UpdateEvent(ctx, eventsdb.UpdateEventParams{
		ID:              eventID,
		Name:            update.Name,
		AmountMinor:     update.AmountMinor,
		DurationSeconds: update.DurationSeconds,
		TotalTickets:    update.TotalTickets,
		AssignedCrewID:  assignedCrewID,
		StartsAt:        timestamp(update.StartsAt),
		EndsAt:          timestamp(update.EndsAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The guarded update found no writable row: the event moved out of
		// 'upcoming' or reservations grew past the requested total.
		return events.Event{}, events.ErrConflict
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("update event: %w", err)
	}
	return eventRecord(record), nil
}

func (store *EventStore) Cancel(ctx context.Context, id string) (events.Event, error) {
	eventID, err := uuid.Parse(id)
	if err != nil {
		return events.Event{}, events.ErrNotFound
	}
	record, err := eventsdb.New(store.pool).CancelEvent(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return events.Event{}, events.ErrNotCancellable
	}
	if err != nil {
		return events.Event{}, fmt.Errorf("cancel event: %w", err)
	}
	return eventRecord(record), nil
}

func (store *EventStore) List(ctx context.Context, filter events.Filter) ([]events.Event, error) {
	var status *eventsdb.EventStatus
	if filter.Status != nil {
		value := eventsdb.EventStatus(*filter.Status)
		status = &value
	}
	var cursorStartsAt pgtype.Timestamptz
	var cursorID pgtype.UUID
	if filter.Cursor != nil {
		cursorStartsAt = timestamp(filter.Cursor.StartsAt)
		cursorID = pgtype.UUID{Bytes: uuid.MustParse(filter.Cursor.ID), Valid: true}
	}

	records, err := eventsdb.New(store.pool).ListEvents(ctx, eventsdb.ListEventsParams{
		Name:           filter.Name,
		Status:         status,
		DurationGte:    filter.DurationGte,
		DurationLte:    filter.DurationLte,
		AmountGte:      filter.AmountGte,
		AmountLte:      filter.AmountLte,
		CursorStartsAt: cursorStartsAt,
		CursorID:       cursorID,
		PageSize:       filter.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	results := make([]events.Event, 0, len(records))
	for _, record := range records {
		results = append(results, eventRecord(record))
	}
	return results, nil
}

func eventRecord(record eventsdb.Event) events.Event {
	return events.Event{
		ID:               record.ID.String(),
		HostID:           record.HostID.String(),
		AssignedCrewID:   optionalString(record.AssignedCrewID),
		Name:             record.Name,
		AmountMinor:      record.AmountMinor,
		DurationSeconds:  record.DurationSeconds,
		Status:           events.Status(record.Status),
		TotalTickets:     record.TotalTickets,
		AvailableTickets: record.AvailableTickets,
		StartsAt:         record.StartsAt.Time,
		EndsAt:           record.EndsAt.Time,
		CancelledAt:      optionalTime(record.CancelledAt),
		CreatedAt:        record.CreatedAt.Time,
		UpdatedAt:        record.UpdatedAt.Time,
	}
}

func eventDetail(record eventsdb.GetEventDetailRow) events.Event {
	return events.Event{
		ID:               record.ID.String(),
		HostID:           record.HostID.String(),
		AssignedCrewID:   optionalString(record.AssignedCrewID),
		Name:             record.Name,
		AmountMinor:      record.AmountMinor,
		DurationSeconds:  record.DurationSeconds,
		Status:           events.Status(record.Status),
		TotalTickets:     record.TotalTickets,
		AvailableTickets: record.AvailableTickets,
		StartsAt:         record.StartsAt.Time,
		EndsAt:           record.EndsAt.Time,
		CancelledAt:      optionalTime(record.CancelledAt),
		CreatedAt:        record.CreatedAt.Time,
		UpdatedAt:        record.UpdatedAt.Time,
	}
}

func timestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func nullableUUID(value *string) (pgtype.UUID, error) {
	if value == nil || *value == "" {
		return pgtype.UUID{}, nil
	}
	id, err := uuid.Parse(*value)
	if err != nil {
		return pgtype.UUID{}, events.ErrInvalidInput
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func optionalString(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	result := uuid.UUID(value.Bytes).String()
	return &result
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func derefAvailability(value *eventsdb.CrewAvailabilityStatus) eventsdb.CrewAvailabilityStatus {
	if value == nil {
		return ""
	}
	return *value
}
