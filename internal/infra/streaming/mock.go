package streaming

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const defaultTokenTTL = 15 * time.Minute

// mock livekit. issues fake tokens, never dials out.
type Mock struct {
	baseURL  string
	tokenTTL time.Duration
}

func NewMock(baseURL string) (*Mock, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("streaming base URL is required")
	}
	return &Mock{baseURL: strings.TrimRight(baseURL, "/"), tokenTTL: defaultTokenTTL}, nil
}

func (mock *Mock) CreateRoom(_ context.Context, roomName string) (Room, error) {
	if strings.TrimSpace(roomName) == "" {
		return Room{}, fmt.Errorf("streaming room name is required")
	}
	return Room{Name: roomName, URL: mock.baseURL + "/rooms/" + roomName}, nil
}

func (mock *Mock) ViewerToken(_ context.Context, roomName, identity string) (ViewerToken, error) {
	if strings.TrimSpace(roomName) == "" || strings.TrimSpace(identity) == "" {
		return ViewerToken{}, ErrAccessDenied
	}
	return ViewerToken{
		Token:     "mock_lk_" + roomName + "_" + identity,
		Room:      roomName,
		URL:       mock.baseURL + "/rooms/" + roomName,
		ExpiresAt: time.Now().Add(mock.tokenTTL),
	}, nil
}

func (mock *Mock) CloseRoom(_ context.Context, roomName string) error {
	if strings.TrimSpace(roomName) == "" {
		return fmt.Errorf("streaming room name is required")
	}
	return nil
}
