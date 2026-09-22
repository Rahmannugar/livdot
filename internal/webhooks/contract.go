package webhooks

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the provider callback route.
func RegisterContract(registry *openapi.Registry) {
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/webhooks/payments", OperationID: "receivePaymentWebhook",
		Summary: "Receive a signed payment provider callback", Tag: "webhooks",
		Responses: map[string]any{
			"200": map[string]any{
				"type":       "object",
				"properties": map[string]any{"status": map[string]any{"type": "string"}},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
