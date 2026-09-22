package crews

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the crew routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	profile := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"accountId":    map[string]any{"type": "string"},
			"name":         map[string]any{"type": "string"},
			"list":         map[string]any{"type": "array", "items": map[string]any{}},
			"availability": map[string]any{"type": "string", "enum": []string{"available", "unavailable"}},
			"updatedAt":    map[string]any{"type": "string", "format": "date-time"},
		},
	}
	page := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"crews":      map[string]any{"type": "array", "items": profile},
			"nextCursor": map[string]any{"type": "string"},
		},
	}
	errors := map[string]any{
		"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
		"403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
	}

	listErrors := map[string]any{}
	for key, value := range errors {
		listErrors[key] = value
	}
	listErrors["200"] = page

	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/crews", OperationID: "listCrews",
		Summary: "Browse crews for event assignment", Tag: "crews",
		Security: true, Roles: []string{"host"}, Responses: listErrors,
	})
	profileErrors := map[string]any{"200": profile, "404": openapi.Ref("Error")}
	for key, value := range errors {
		profileErrors[key] = value
	}
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/account/crews", OperationID: "getCrewProfile",
		Summary: "Read the caller's crew profile", Tag: "crews",
		Security: true, Roles: []string{"crew"}, Responses: profileErrors,
	})
	updateResponses := map[string]any{"200": profile}
	for key, value := range errors {
		updateResponses[key] = value
	}
	registry.Add(openapi.Operation{
		Method: "patch", Path: "/api/account/crews", OperationID: "updateCrewProfile",
		Summary: "Update the caller's crew profile", Tag: "crews",
		Security: true, Roles: []string{"crew"},
		Request: updateProfileRequest{}, RequestName: "UpdateCrewProfileRequest",
		Responses: updateResponses,
	})
}
