package streamingrepo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Rahmannugar/livdot/internal/streaming"
	streamingdb "github.com/Rahmannugar/livdot/internal/streaming/repositories/generated"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type StreamStore struct {
	pool *pgxpool.Pool
}

func NewStreamStore(pool *pgxpool.Pool) *StreamStore {
	return &StreamStore{pool: pool}
}

func (store *StreamStore) Event(ctx context.Context, eventID string) (streaming.Event, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Event{}, streaming.ErrNotFound
	}
	record, err := streamingdb.New(store.pool).GetEventForStream(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Event{}, streaming.ErrNotFound
	}
	if err != nil {
		return streaming.Event{}, fmt.Errorf("get event for stream: %w", err)
	}
	return streaming.Event{
		ID:              record.ID.String(),
		HostID:          record.HostID.String(),
		Status:          string(record.Status),
		DurationSeconds: record.DurationSeconds,
		StartsAt:        record.StartsAt.Time,
		EndsAt:          record.EndsAt.Time,
	}, nil
}

func (store *StreamStore) ActiveMember(ctx context.Context, eventID, userID string) (streaming.Member, error) {
	event, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Member{}, streaming.ErrInvalidInput
	}
	user, err := uuid.Parse(userID)
	if err != nil {
		return streaming.Member{}, streaming.ErrInvalidInput
	}
	record, err := streamingdb.New(store.pool).GetActiveMember(ctx, streamingdb.GetActiveMemberParams{
		EventID: event,
		UserID:  user,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Member{}, streaming.ErrNotMember
	}
	if err != nil {
		return streaming.Member{}, fmt.Errorf("get active member: %w", err)
	}
	return streaming.Member{ID: record.ID.String(), TicketID: record.TicketID.String()}, nil
}

// Start flips the event live and opens the stream in one tx. The guarded status
// update means two concurrent starts cannot both create a room.
func (store *StreamStore) Start(ctx context.Context, eventID, roomID string, startedAt time.Time) (streaming.Stream, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	streamID, err := uuid.NewV7()
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("generate stream id: %w", err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("begin stream start: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := streamingdb.New(tx)
	if _, err := queries.MarkEventLive(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return streaming.Stream{}, streaming.ErrNotStartable
		}
		return streaming.Stream{}, fmt.Errorf("mark event live: %w", err)
	}
	record, err := queries.CreateEventStream(ctx, streamingdb.CreateEventStreamParams{
		ID:            streamID,
		EventID:       id,
		LivekitRoomID: roomID,
		StartedAt:     timestamp(startedAt),
	})
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("create event stream: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return streaming.Stream{}, fmt.Errorf("commit stream start: %w", err)
	}
	return streamFromRecord(record), nil
}

func (store *StreamStore) LiveStream(ctx context.Context, eventID string) (streaming.Stream, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	record, err := streamingdb.New(store.pool).GetEventStreamByEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("get event stream: %w", err)
	}
	if record.Status != streamingdb.StreamStatusLive {
		return streaming.Stream{}, streaming.ErrNotLive
	}
	return streamFromRecord(record), nil
}

func (store *StreamStore) RecordJoin(ctx context.Context, streamID, memberID string, joinedAt time.Time) error {
	stream, err := uuid.Parse(streamID)
	if err != nil {
		return streaming.ErrInvalidInput
	}
	member, err := uuid.Parse(memberID)
	if err != nil {
		return streaming.ErrInvalidInput
	}
	joinID, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("generate stream member id: %w", err)
	}
	if _, err := streamingdb.New(store.pool).RecordStreamMemberJoin(ctx, streamingdb.RecordStreamMemberJoinParams{
		ID:            joinID,
		StreamID:      stream,
		EventMemberID: member,
		JoinedAt:      timestamp(joinedAt),
	}); err != nil {
		return fmt.Errorf("record stream member join: %w", err)
	}
	return nil
}

// End closes the stream and ends the event in one tx.
func (store *StreamStore) End(ctx context.Context, eventID string, finishedAt time.Time) (streaming.Stream, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("begin stream end: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := streamingdb.New(tx)
	current, err := queries.GetEventStreamByEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("get event stream: %w", err)
	}
	record, err := queries.RecordStreamEnd(ctx, streamingdb.RecordStreamEndParams{
		ID:         current.ID,
		FinishedAt: timestamp(finishedAt),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, streaming.ErrNotLive
	}
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("record stream end: %w", err)
	}
	if _, err := queries.MarkEventEnded(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, fmt.Errorf("mark event ended: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return streaming.Stream{}, fmt.Errorf("commit stream end: %w", err)
	}
	return streamFromRecord(record), nil
}

// Fail records the failure and ends the event in one tx.
func (store *StreamStore) Fail(ctx context.Context, eventID string, failedAt time.Time, reason string) (streaming.Stream, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("begin stream failure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	queries := streamingdb.New(tx)
	current, err := queries.GetEventStreamByEvent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, streaming.ErrNotFound
	}
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("get event stream: %w", err)
	}
	var failureReason *string
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		failureReason = &trimmed
	}
	record, err := queries.RecordStreamFailure(ctx, streamingdb.RecordStreamFailureParams{
		ID:            current.ID,
		FinishedAt:    timestamp(failedAt),
		FailureReason: failureReason,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, streaming.ErrNotLive
	}
	if err != nil {
		return streaming.Stream{}, fmt.Errorf("record stream failure: %w", err)
	}
	if _, err := queries.MarkEventEnded(ctx, id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return streaming.Stream{}, fmt.Errorf("mark event ended: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return streaming.Stream{}, fmt.Errorf("commit stream failure: %w", err)
	}
	return streamFromRecord(record), nil
}

func streamFromRecord(record streamingdb.EventStream) streaming.Stream {
	return streaming.Stream{
		ID:            record.ID.String(),
		EventID:       record.EventID.String(),
		RoomID:        record.LivekitRoomID,
		Status:        string(record.Status),
		StartedAt:     record.StartedAt.Time,
		FinishedAt:    optionalTime(record.FinishedAt),
		FailedAt:      optionalTime(record.FailedAt),
		FailureReason: record.FailureReason,
	}
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
