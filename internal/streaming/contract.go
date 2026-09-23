package streaming

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the stream lifecycle routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("Stream", map[string]any{
		"type":     "object",
		"required": []string{"id", "eventId", "roomId", "status", "startedAt"},
		"properties": map[string]any{
			"id":            map[string]any{"type": "string", "example": "018f2c1e-dddd-7c3b-9d4e-2b8a1c5f7e90"},
			"eventId":       map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"roomId":        map[string]any{"type": "string", "example": "livdot-018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"status":        map[string]any{"type": "string", "enum": []string{"live", "failed", "ended"}, "example": "live"},
			"startedAt":     map[string]any{"type": "string", "format": "date-time", "example": "2026-12-01T18:00:00Z"},
			"finishedAt":    map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": nil},
			"failedAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": nil},
			"failureReason": map[string]any{"type": "string", "nullable": true, "example": nil},
		},
	})
	registry.Component("StreamSession", map[string]any{
		"type":     "object",
		"required": []string{"token", "room", "url", "expiresAt"},
		"properties": map[string]any{
			"token":     map[string]any{"type": "string", "example": "mock_lk_livdot-018f2c1e-9a11-7c3b_018f2c1e-bbbb"},
			"room":      map[string]any{"type": "string", "example": "livdot-018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"url":       map[string]any{"type": "string", "example": "https://mock.livekit.local/rooms/livdot-018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"expiresAt": map[string]any{"type": "string", "format": "date-time", "example": "2026-12-01T18:15:00Z"},
		},
	})
	control := map[string]any{
		"200": openapi.Ref("Stream"), "400": openapi.Ref("Error"),
		"401": openapi.Ref("Error"), "403": openapi.Ref("Error"),
		"404": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
	}
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/start", OperationID: "startStream",
		Summary: "Start an event stream", Tag: "streaming",
		Security: true, Roles: []string{"host"}, Responses: control,
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/end", OperationID: "endStream",
		Summary: "End an event stream", Tag: "streaming",
		Security: true, Roles: []string{"host"}, Responses: control,
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/stream-failure", OperationID: "failStream",
		Summary: "Record a stream failure", Tag: "streaming",
		Security: true, Roles: []string{"host"},
		Request: failureRequest{}, RequestName: "StreamFailureRequest",
		Responses: control,
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/join", OperationID: "joinStream",
		Summary: "Join a live stream as a ticket holder", Tag: "streaming",
		Security: true, Roles: []string{"user"},
		Responses: map[string]any{
			"200": openapi.Ref("StreamSession"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
