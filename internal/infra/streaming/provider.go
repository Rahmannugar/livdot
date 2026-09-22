// Package streaming is the realtime boundary (mocked livekit).
package streaming

import (
	"context"
	"errors"
	"time"
)

var (
	ErrRoomUnavailable = errors.New("streaming room is unavailable")
	ErrAccessDenied    = errors.New("streaming access is denied")
)

// the provider room for one event stream.
type Room struct {
	Name string
	URL  string
}

// short-lived credential a member joins with.
type ViewerToken struct {
	Token     string
	Room      string
	URL       string
	ExpiresAt time.Time
}

// realtime provider. room names are ours so access stays server-side.
type Provider interface {
	CreateRoom(ctx context.Context, roomName string) (Room, error)
	ViewerToken(ctx context.Context, roomName, identity string) (ViewerToken, error)
	CloseRoom(ctx context.Context, roomName string) error
}
