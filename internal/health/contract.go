package health

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the liveness and readiness probes.
func RegisterContract(registry *openapi.Registry) {
	status := map[string]any{
		"type":       "object",
		"properties": map[string]any{"status": map[string]any{"type": "string"}},
	}
	readiness := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"status": map[string]any{"type": "string"},
			"dependencies": map[string]any{
				"type":                 "object",
				"additionalProperties": map[string]any{"type": "string"},
			},
		},
	}
	registry.Add(openapi.Operation{
		Method: "get", Path: "/health/live", OperationID: "liveness",
		Summary: "Liveness probe", Tag: "health", Responses: map[string]any{"200": status},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/health/ready", OperationID: "readiness",
		Summary: "Readiness probe with per-dependency status", Tag: "health",
		Responses: map[string]any{"200": readiness, "503": readiness},
	})
}
