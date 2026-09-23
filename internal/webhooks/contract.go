package webhooks

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the provider callback route.
func RegisterContract(registry *openapi.Registry) {
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/webhooks/payments", OperationID: "receivePaymentWebhook",
		Summary: "Receive a signed payment provider callback", Tag: "webhooks",
		Responses: map[string]any{
			"200": map[string]any{
				"type":     "object",
				"required": []string{"status"},
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "example": "received"},
				},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/dev/payments/notify", OperationID: "simulatePayment",
		Summary: "Development only: deliver a signed mocked payment callback",
		Tag:     "webhooks",
		Request: simulatePaymentRequest{}, RequestName: "SimulatePaymentRequest",
		Responses: map[string]any{
			"200": map[string]any{
				"type":     "object",
				"required": []string{"status", "type"},
				"properties": map[string]any{
					"status": map[string]any{"type": "string", "example": "delivered"},
					"type":   map[string]any{"type": "string", "enum": []string{"charge.processing", "charge.paid", "charge.failed"}, "example": "charge.paid"},
				},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
