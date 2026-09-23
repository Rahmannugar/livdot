package crews

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the crew routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("CrewProfile", map[string]any{
		"type":     "object",
		"required": []string{"accountId", "name", "list", "availability", "updatedAt"},
		"properties": map[string]any{
			"accountId":    map[string]any{"type": "string", "example": "018f2c1e-6f2a-7c3b-9d4e-2b8a1c5f7e90"},
			"name":         map[string]any{"type": "string", "example": "Crew One"},
			"list":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "example": []string{"camera-op", "audio-op", "switcher"}},
			"availability": map[string]any{"type": "string", "enum": []string{"available", "unavailable"}, "example": "available"},
			"updatedAt":    map[string]any{"type": "string", "format": "date-time", "example": "2026-09-23T17:55:00Z"},
		},
	})
	registry.Component("CrewPage", map[string]any{
		"type":     "object",
		"required": []string{"crews", "nextCursor"},
		"properties": map[string]any{
			"crews":      map[string]any{"type": "array", "items": openapi.Ref("CrewProfile")},
			"nextCursor": map[string]any{"type": "string", "example": "eyJzIjoiQ3JldyBPbmUiLCJpIjoiMDE4ZjJjMWUtNmYyYS03YzNiLTlkNGUtMmI4YTFjNWY3ZTkwIn0"},
		},
	})

	errors := map[string]any{
		"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
		"403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
	}

	list := map[string]any{"200": openapi.Ref("CrewPage")}
	for key, value := range errors {
		list[key] = value
	}
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/crews", OperationID: "listCrews",
		Summary: "Browse crews for event assignment", Tag: "crews",
		Security: true, Roles: []string{"host"}, Responses: list,
	})

	profile := map[string]any{"200": openapi.Ref("CrewProfile"), "404": openapi.Ref("Error")}
	for key, value := range errors {
		profile[key] = value
	}
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/account/crews", OperationID: "getCrewProfile",
		Summary: "Read the caller's crew profile", Tag: "crews",
		Security: true, Roles: []string{"crew"}, Responses: profile,
	})
	registry.Add(openapi.Operation{
		Method: "patch", Path: "/api/account/crews", OperationID: "updateCrewProfile",
		Summary: "Update the caller's crew profile", Tag: "crews",
		Security: true, Roles: []string{"crew"},
		Request: updateProfileRequest{}, RequestName: "UpdateCrewProfileRequest",
		Responses: profile,
	})
}
