// Package streaming owns the event stream lifecycle and viewer access.
package streaming

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	streamprovider "github.com/Rahmannugar/livdot/internal/infra/streaming"
	"github.com/Rahmannugar/livdot/internal/validation"
)

// failures in the first quarter(25%) of the scheduled duration auto-refund; later
// ones need admin review.
const refundThresholdDivisor = 4

const maxFailureReasonRunes = 1000

const (
	EventUpcoming = "upcoming"
	EventLive     = "live"
	EventEnded    = "ended"
)

var (
	ErrNotFound     = errors.New("event or stream not found")
	ErrInvalidInput = errors.New("invalid streaming input")
	ErrForbidden    = errors.New("stream belongs to another host")
	ErrNotStartable = errors.New("event cannot start in its current state")
	ErrNotLive      = errors.New("event is not live")
	ErrNotMember    = errors.New("only ticket holders can join the stream")
)

// a stream as the streaming domain sees it.
type Stream struct {
	ID            string
	EventID       string
	RoomID        string
	Status        string
	StartedAt     time.Time
	FinishedAt    *time.Time
	FailedAt      *time.Time
	FailureReason *string
}

// join credentials handed to a member.
type Session struct {
	Token     string
	Room      string
	URL       string
	ExpiresAt time.Time
}

// event facts the lifecycle needs.
type Event struct {
	ID              string
	HostID          string
	Status          string
	DurationSeconds int32
	StartsAt        time.Time
	EndsAt          time.Time
}

// member facts for a join.
type Member struct {
	ID       string
	TicketID string
}

// the settlement work streaming asks finance to do.
type Settlements interface {
	RefundEvent(ctx context.Context, eventID string, automatic bool) (int, error)
	AccruePayout(ctx context.Context, eventID string) error
}

// persistence port owned by the streaming domain.
type Store interface {
	Event(ctx context.Context, eventID string) (Event, error)
	ActiveMember(ctx context.Context, eventID, userID string) (Member, error)
	Start(ctx context.Context, eventID, roomID string, startedAt time.Time) (Stream, error)
	LiveStream(ctx context.Context, eventID string) (Stream, error)
	RecordJoin(ctx context.Context, streamID, memberID string, joinedAt time.Time) error
	End(ctx context.Context, eventID string, finishedAt time.Time) (Stream, error)
	Fail(ctx context.Context, eventID string, failedAt time.Time, reason string) (Stream, error)
}

type Service struct {
	store       Store
	provider    streamprovider.Provider
	settlements Settlements
	now         func() time.Time
}

func NewService(store Store, provider streamprovider.Provider, settlements Settlements) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("streaming store is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("streaming provider is required")
	}
	if settlements == nil {
		return nil, fmt.Errorf("stream settlements are required")
	}
	return &Service{store: store, provider: provider, settlements: settlements, now: time.Now}, nil
}

// Start opens the provider room and moves the event live.
func (service *Service) Start(ctx context.Context, hostID, eventID string) (Stream, error) {
	event, err := service.store.Event(ctx, eventID)
	if err != nil {
		return Stream{}, err
	}
	if event.HostID != hostID {
		return Stream{}, ErrForbidden
	}
	if event.Status != EventUpcoming {
		return Stream{}, ErrNotStartable
	}
	roomID := "livdot-" + eventID
	if _, err := service.provider.CreateRoom(ctx, roomID); err != nil {
		return Stream{}, fmt.Errorf("create stream room: %w", err)
	}
	return service.store.Start(ctx, eventID, roomID, service.now())
}

// Join issues a viewer token, but only to an active ticket holder.
func (service *Service) Join(ctx context.Context, userID, eventID string) (Session, error) {
	event, err := service.store.Event(ctx, eventID)
	if err != nil {
		return Session{}, err
	}
	if event.Status != EventLive {
		return Session{}, ErrNotLive
	}
	member, err := service.store.ActiveMember(ctx, eventID, userID)
	if err != nil {
		return Session{}, err
	}
	stream, err := service.store.LiveStream(ctx, eventID)
	if err != nil {
		return Session{}, err
	}
	token, err := service.provider.ViewerToken(ctx, stream.RoomID, userID)
	if err != nil {
		return Session{}, fmt.Errorf("issue viewer token: %w", err)
	}
	if err := service.store.RecordJoin(ctx, stream.ID, member.ID, service.now()); err != nil {
		return Session{}, err
	}
	return Session{Token: token.Token, Room: token.Room, URL: token.URL, ExpiresAt: token.ExpiresAt}, nil
}

// End closes the stream, then makes the event's payout available.
func (service *Service) End(ctx context.Context, hostID, eventID string) (Stream, error) {
	event, err := service.store.Event(ctx, eventID)
	if err != nil {
		return Stream{}, err
	}
	if event.HostID != hostID {
		return Stream{}, ErrForbidden
	}
	if event.Status != EventLive {
		return Stream{}, ErrNotLive
	}
	stream, err := service.store.End(ctx, eventID, service.now())
	if err != nil {
		return Stream{}, err
	}
	if err := service.settlements.AccruePayout(ctx, eventID); err != nil {
		// the event is already ended; reconciliation retries the payout later.
		slog.Warn("payout accrual failed", "event_id", eventID, "error", err)
	}
	return stream, nil
}

// Fail records the failure and decides refund versus admin review. A failure
// in the first quarter of the scheduled duration refunds everyone automatically.
func (service *Service) Fail(ctx context.Context, hostID, eventID, reason string) (Stream, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Stream{}, validation.New(ErrInvalidInput, "reason", "reason is required")
	}
	if utf8.RuneCountInString(reason) > maxFailureReasonRunes {
		return Stream{}, validation.New(ErrInvalidInput, "reason", "reason must be 1000 characters or fewer")
	}
	event, err := service.store.Event(ctx, eventID)
	if err != nil {
		return Stream{}, err
	}
	if event.HostID != hostID {
		return Stream{}, ErrForbidden
	}
	if event.Status != EventLive {
		return Stream{}, ErrNotLive
	}
	now := service.now()
	stream, err := service.store.Fail(ctx, eventID, now, reason)
	if err != nil {
		return Stream{}, err
	}
	if stream.StartedAt.IsZero() {
		return stream, nil
	}
	threshold := time.Duration(event.DurationSeconds) * time.Second / refundThresholdDivisor
	if now.Sub(stream.StartedAt) < threshold {
		count, err := service.settlements.RefundEvent(ctx, eventID, true)
		if err != nil {
			slog.Warn("automatic refunds failed", "event_id", eventID, "error", err)
			return stream, nil
		}
		slog.Info("automatic refunds issued", "event_id", eventID, "count", count)
		return stream, nil
	}
	slog.Info("stream failed after refund threshold; admin review required", "event_id", eventID)
	return stream, nil
}
