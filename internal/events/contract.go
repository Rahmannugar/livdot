package events

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the event routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("EventCrew", map[string]any{
		"type":     "object",
		"required": []string{"accountId", "name", "availability"},
		"properties": map[string]any{
			"accountId":    map[string]any{"type": "string", "example": "018f2c1e-6f2a-7c3b-9d4e-2b8a1c5f7e90"},
			"name":         map[string]any{"type": "string", "example": "Crew One"},
			"availability": map[string]any{"type": "string", "enum": []string{"available", "unavailable"}, "example": "available"},
		},
	})
	registry.Component("Event", map[string]any{
		"type": "object",
		"required": []string{
			"id", "hostId", "name", "amountMinor", "durationSeconds", "status",
			"totalTickets", "availableTickets", "startsAt", "endsAt", "createdAt", "updatedAt",
		},
		"properties": map[string]any{
			"id":               map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"hostId":           map[string]any{"type": "string", "example": "018f2c1e-1111-7c3b-9d4e-2b8a1c5f7e90"},
			"assignedCrewId":   map[string]any{"type": "string", "nullable": true, "example": "018f2c1e-6f2a-7c3b-9d4e-2b8a1c5f7e90"},
			"assignedCrew":     openapi.Ref("EventCrew"),
			"name":             map[string]any{"type": "string", "example": "Live Show"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64", "example": 500000},
			"durationSeconds":  map[string]any{"type": "integer", "example": 3600},
			"status":           map[string]any{"type": "string", "enum": []string{"upcoming", "live", "ended", "cancelled"}, "example": "upcoming"},
			"totalTickets":     map[string]any{"type": "integer", "example": 100},
			"availableTickets": map[string]any{"type": "integer", "example": 99},
			"startsAt":         map[string]any{"type": "string", "format": "date-time", "example": "2026-12-01T18:00:00Z"},
			"endsAt":           map[string]any{"type": "string", "format": "date-time", "example": "2026-12-01T19:00:00Z"},
			"cancelledAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": nil},
			"createdAt":        map[string]any{"type": "string", "format": "date-time", "example": "2026-09-23T17:50:00Z"},
			"updatedAt":        map[string]any{"type": "string", "format": "date-time", "example": "2026-09-23T17:50:00Z"},
			"purchased":        map[string]any{"type": "boolean", "description": "Viewer only: whether the account holds paid access", "example": false},
		},
	})
	registry.Component("EventPage", map[string]any{
		"type":     "object",
		"required": []string{"events", "nextCursor"},
		"properties": map[string]any{
			"events":     map[string]any{"type": "array", "items": openapi.Ref("Event")},
			"nextCursor": map[string]any{"type": "string", "example": "eyJzIjoiMjAyNi0xMi0wMVQxODowMDowMFoiLCJpIjoiMDE4ZjJjMWUtOWExMS03YzNiLTlkNGUtMmI4YTFjNWY3ZTkwIn0"},
		},
	})

	errors := map[string]any{
		"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
		"403": openapi.Ref("Error"), "404": openapi.Ref("Error"),
		"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
	}

	list := map[string]any{"200": openapi.Ref("EventPage")}
	for key, value := range errors {
		list[key] = value
	}
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/events", OperationID: "listEvents",
		Summary: "List events with filters", Tag: "events",
		Security: true, Responses: list,
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/events/{id}", OperationID: "getEvent",
		Summary: "Get an event and its details", Tag: "events",
		Security: true, Responses: list,
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events", OperationID: "createEvent",
		Summary: "Create an event", Tag: "events", Security: true, Roles: []string{"host"},
		Request: createEventRequest{}, RequestName: "CreateEventRequest",
		Responses: map[string]any{"201": openapi.Ref("Event"), "400": openapi.Ref("Error"),
			"401": openapi.Ref("Error"), "403": openapi.Ref("Error"), "429": openapi.Ref("Error")},
	})
	registry.Add(openapi.Operation{
		Method: "patch", Path: "/api/events/{id}", OperationID: "updateEvent",
		Summary: "Update or cancel an event", Tag: "events", Security: true, Roles: []string{"host"},
		Request: updateEventRequest{}, RequestName: "UpdateEventRequest",
		Responses: map[string]any{"200": openapi.Ref("Event"), "400": openapi.Ref("Error"),
			"401": openapi.Ref("Error"), "403": openapi.Ref("Error"), "404": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error")},
	})
}
