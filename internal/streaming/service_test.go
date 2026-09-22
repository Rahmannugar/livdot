package streaming

import (
	"context"
	"testing"
	"time"

	streamprovider "github.com/Rahmannugar/livdot/internal/infra/streaming"
)

type stubStore struct {
	event  Event
	stream Stream
}

func (store *stubStore) Event(context.Context, string) (Event, error) { return store.event, nil }
func (store *stubStore) ActiveMember(context.Context, string, string) (Member, error) {
	return Member{}, nil
}
func (store *stubStore) Start(context.Context, string, string, time.Time) (Stream, error) {
	return Stream{}, nil
}
func (store *stubStore) LiveStream(context.Context, string) (Stream, error) { return Stream{}, nil }
func (store *stubStore) RecordJoin(context.Context, string, string, time.Time) error {
	return nil
}
func (store *stubStore) End(context.Context, string, time.Time) (Stream, error) { return Stream{}, nil }
func (store *stubStore) Fail(context.Context, string, time.Time, string) (Stream, error) {
	return store.stream, nil
}

type spySettlements struct {
	refundCalls []bool
}

func (fake *spySettlements) RefundEvent(_ context.Context, _ string, automatic bool) (int, error) {
	fake.refundCalls = append(fake.refundCalls, automatic)
	return 1, nil
}

func (fake *spySettlements) AccruePayout(context.Context, string) error { return nil }

type stubProvider struct{}

func (stubProvider) CreateRoom(context.Context, string) (streamprovider.Room, error) {
	return streamprovider.Room{}, nil
}
func (stubProvider) ViewerToken(context.Context, string, string) (streamprovider.ViewerToken, error) {
	return streamprovider.ViewerToken{}, nil
}
func (stubProvider) CloseRoom(context.Context, string) error { return nil }

// TestFailRefundsOnlyInFirstQuarter covers the 25% rule: a failure early in the
// scheduled duration refunds automatically, a later one does not.
func TestFailRefundsOnlyInFirstQuarter(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const host = "host-1"
	const eventID = "event-1"

	tests := []struct {
		name       string
		elapsed    time.Duration
		wantRefund bool
	}{
		{name: "before threshold", elapsed: 10 * time.Second, wantRefund: true},
		{name: "after threshold", elapsed: 40 * time.Second, wantRefund: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &stubStore{
				event:  Event{ID: eventID, HostID: host, Status: EventLive, DurationSeconds: 100},
				stream: Stream{StartedAt: fixed.Add(-test.elapsed)},
			}
			settlements := &spySettlements{}
			service, err := NewService(store, stubProvider{}, settlements)
			if err != nil {
				t.Fatalf("NewService() error = %v", err)
			}
			service.now = func() time.Time { return fixed }

			if _, err := service.Fail(context.Background(), host, eventID, "encoder down"); err != nil {
				t.Fatalf("Fail() error = %v", err)
			}
			gotRefund := len(settlements.refundCalls) == 1
			if gotRefund != test.wantRefund {
				t.Fatalf("RefundEvent called = %v, want %v", gotRefund, test.wantRefund)
			}
		})
	}
}

func TestFailRejectsForeignHost(t *testing.T) {
	store := &stubStore{event: Event{HostID: "host-1", Status: EventLive, DurationSeconds: 100}}
	settlements := &spySettlements{}
	service, err := NewService(store, stubProvider{}, settlements)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	if _, err := service.Fail(context.Background(), "host-2", "event-1", "boom"); err != ErrForbidden {
		t.Fatalf("Fail() error = %v, want ErrForbidden", err)
	}
	if len(settlements.refundCalls) != 0 {
		t.Fatal("RefundEvent should not run for a foreign host")
	}
}
