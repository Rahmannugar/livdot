package events

import (
	"errors"
	"testing"
	"time"

	"github.com/Rahmannugar/livdot/internal/validation"
)

func TestValidateScheduleReturnsSafeFieldDetails(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		eventName       string
		amountMinor     int64
		durationSeconds int32
		totalTickets    int32
		startsAt        time.Time
		wantField       string
		wantMessage     string
	}{
		{
			name: "missing name", eventName: "", amountMinor: 1000,
			durationSeconds: 3600, totalTickets: 10, startsAt: now.Add(time.Hour),
			wantField: "name", wantMessage: "name is required",
		},
		{
			name: "negative amount", eventName: "Event", amountMinor: -1,
			durationSeconds: 3600, totalTickets: 10, startsAt: now.Add(time.Hour),
			wantField: "amountMinor", wantMessage: "amountMinor must be zero or greater",
		},
		{
			name: "past start", eventName: "Event", amountMinor: 1000,
			durationSeconds: 3600, totalTickets: 10, startsAt: now.Add(-time.Second),
			wantField: "startsAt", wantMessage: "startsAt must be a future date and time",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSchedule(test.eventName, test.amountMinor, test.durationSeconds,
				test.totalTickets, test.startsAt, now)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("validateSchedule() error = %v, want ErrInvalidInput", err)
			}
			field, message, ok := validation.Details(err)
			if !ok || field != test.wantField || message != test.wantMessage {
				t.Fatalf("validation details = (%q, %q, %v), want (%q, %q, true)",
					field, message, ok, test.wantField, test.wantMessage)
			}
		})
	}
}
