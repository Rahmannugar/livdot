package events

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the event routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	event := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string"},
			"hostId":           map[string]any{"type": "string"},
			"assignedCrewId":   map[string]any{"type": "string", "nullable": true},
			"assignedCrew":     map[string]any{"type": "object"},
			"name":             map[string]any{"type": "string"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64"},
			"durationSeconds":  map[string]any{"type": "integer"},
			"status":           map[string]any{"type": "string", "enum": []string{"upcoming", "live", "ended", "cancelled"}},
			"totalTickets":     map[string]any{"type": "integer"},
			"availableTickets": map[string]any{"type": "integer"},
			"startsAt":         map[string]any{"type": "string", "format": "date-time"},
			"endsAt":           map[string]any{"type": "string", "format": "date-time"},
			"cancelledAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"createdAt":        map[string]any{"type": "string", "format": "date-time"},
			"updatedAt":        map[string]any{"type": "string", "format": "date-time"},
		},
	}
	page := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"events":     map[string]any{"type": "array", "items": event},
			"nextCursor": map[string]any{"type": "string"},
		},
	}
	list := responses("200", page)
	list["404"] = openapi.Ref("Error")
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/events", OperationID: "listEvents",
		Summary: "List events with filters", Tag: "events", Responses: list,
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/events/{id}", OperationID: "getEvent",
		Summary: "Get an event and its details", Tag: "events", Responses: responses("200", event, "404"),
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events", OperationID: "createEvent",
		Summary: "Create an event", Tag: "events", Security: true, Roles: []string{"host"},
		Request: createEventRequest{}, RequestName: "CreateEventRequest",
		Responses: responses("201", event, "400", "403"),
	})
	registry.Add(openapi.Operation{
		Method: "patch", Path: "/api/events/{id}", OperationID: "updateEvent",
		Summary: "Update or cancel an event", Tag: "events", Security: true, Roles: []string{"host"},
		Request: updateEventRequest{}, RequestName: "UpdateEventRequest",
		Responses: responses("200", event, "400", "403", "404", "409"),
	})
}

// responses builds a status->schema map, adding the shared error schema for each
// error status named.
func responses(successStatus string, success any, errorStatuses ...string) map[string]any {
	result := map[string]any{successStatus: success}
	for _, status := range errorStatuses {
		result[status] = openapi.Ref("Error")
	}
	result["429"] = openapi.Ref("Error")
	return result
}
