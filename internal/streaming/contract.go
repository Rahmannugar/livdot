package streaming

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the stream lifecycle routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	stream := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":            map[string]any{"type": "string"},
			"eventId":       map[string]any{"type": "string"},
			"roomId":        map[string]any{"type": "string"},
			"status":        map[string]any{"type": "string", "enum": []string{"live", "failed", "ended"}},
			"startedAt":     map[string]any{"type": "string", "format": "date-time"},
			"finishedAt":    map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"failedAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"failureReason": map[string]any{"type": "string", "nullable": true},
		},
	}
	session := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"token":     map[string]any{"type": "string"},
			"room":      map[string]any{"type": "string"},
			"url":       map[string]any{"type": "string"},
			"expiresAt": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	control := map[string]any{
		"200": stream, "400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
		"403": openapi.Ref("Error"), "404": openapi.Ref("Error"),
		"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
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
			"200": session, "401": openapi.Ref("Error"), "403": openapi.Ref("Error"),
			"404": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
