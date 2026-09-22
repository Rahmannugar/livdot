// Package openapi aggregates every domain's contract into one document.
package openapi

import (
	"github.com/Rahmannugar/livdot/internal/authentication"
	"github.com/Rahmannugar/livdot/internal/crews"
	"github.com/Rahmannugar/livdot/internal/events"
	"github.com/Rahmannugar/livdot/internal/finance"
	"github.com/Rahmannugar/livdot/internal/health"
	openapilib "github.com/Rahmannugar/livdot/internal/infra/openapi"
	"github.com/Rahmannugar/livdot/internal/streaming"
	"github.com/Rahmannugar/livdot/internal/ticketing"
	"github.com/Rahmannugar/livdot/internal/webhooks"
)

const (
	title       = "LIV DOT API"
	version     = "1.0.0"
	description = "Backend for paid live events: event lifecycle, ticket purchase, mocked payment, refunds, and payouts."
)

// Document builds the full OpenAPI 3 document. Requests are generated from the
// runtime request structs; responses are the hand-maintained schemas each domain
// registers.
func Document() map[string]any {
	registry := openapilib.NewRegistry()
	registry.Component("Error", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":    map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
				},
			},
		},
	})

	authentication.RegisterContract(registry)
	crews.RegisterContract(registry)
	events.RegisterContract(registry)
	ticketing.RegisterContract(registry)
	streaming.RegisterContract(registry)
	finance.RegisterContract(registry)
	webhooks.RegisterContract(registry)
	health.RegisterContract(registry)

	return registry.Document(title, version, description)
}
