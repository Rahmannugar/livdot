package finance

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the refund and payout routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("Refund", map[string]any{
		"type": "object",
		"required": []string{
			"id", "eventId", "userId", "purchaseId", "amountMinor", "status",
		},
		"properties": map[string]any{
			"id":               map[string]any{"type": "string", "example": "018f2c1e-eeee-7c3b-9d4e-2b8a1c5f7e90"},
			"eventId":          map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"userId":           map[string]any{"type": "string", "example": "018f2c1e-bbbb-7c3b-9d4e-2b8a1c5f7e90"},
			"purchaseId":       map[string]any{"type": "string", "example": "018f2c1e-aaaa-7c3b-9d4e-2b8a1c5f7e90"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64", "example": 500000},
			"status":           map[string]any{"type": "string", "enum": []string{"processing", "refunded", "failed"}, "example": "refunded"},
			"providerRefundId": map[string]any{"type": "string", "nullable": true, "example": "mock_rfnd_018f2c1e-eeee-7c3b-9d4e-2b8a1c5f7e90"},
			"refundedAt":       map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": "2026-12-01T18:02:00Z"},
		},
	})
	registry.Component("RefundPage", map[string]any{
		"type":     "object",
		"required": []string{"refunds", "nextCursor"},
		"properties": map[string]any{
			"refunds":    map[string]any{"type": "array", "items": openapi.Ref("Refund")},
			"nextCursor": map[string]any{"type": "string", "example": ""},
		},
	})
	registry.Component("Payout", map[string]any{
		"type": "object",
		"required": []string{
			"id", "eventId", "hostId", "amountMinor", "status",
		},
		"properties": map[string]any{
			"id":               map[string]any{"type": "string", "example": "018f2c1e-ffff-7c3b-9d4e-2b8a1c5f7e90"},
			"eventId":          map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"hostId":           map[string]any{"type": "string", "example": "018f2c1e-1111-7c3b-9d4e-2b8a1c5f7e90"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64", "example": 500000},
			"status":           map[string]any{"type": "string", "enum": []string{"processing", "paid", "failed"}, "example": "paid"},
			"providerPayoutId": map[string]any{"type": "string", "nullable": true, "example": "mock_pyt_018f2c1e-ffff-7c3b-9d4e-2b8a1c5f7e90"},
			"paidAt":           map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": "2026-12-01T19:01:00Z"},
		},
	})
	registry.Component("PayoutPage", map[string]any{
		"type":     "object",
		"required": []string{"payouts", "nextCursor"},
		"properties": map[string]any{
			"payouts":    map[string]any{"type": "array", "items": openapi.Ref("Payout")},
			"nextCursor": map[string]any{"type": "string", "example": ""},
		},
	})

	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/refund", OperationID: "refundEvent",
		Summary: "Refund a failed event's viewers", Tag: "finance",
		Security: true, Roles: []string{"internal_admin"},
		Responses: map[string]any{
			"202": map[string]any{
				"type":     "object",
				"required": []string{"refunds"},
				"properties": map[string]any{
					"refunds": map[string]any{"type": "integer", "example": 12},
				},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/refunds", OperationID: "listRefunds",
		Summary: "List refunds", Tag: "finance",
		Security: true, Roles: []string{"user", "internal_admin"},
		Responses: map[string]any{
			"200": openapi.Ref("RefundPage"), "400": openapi.Ref("Error"),
			"401": openapi.Ref("Error"), "403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/refunds/{id}", OperationID: "getRefund",
		Summary: "Get one refund", Tag: "finance",
		Security: true, Roles: []string{"user", "internal_admin"},
		Responses: map[string]any{
			"200": openapi.Ref("Refund"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/payouts", OperationID: "listPayouts",
		Summary: "List payouts", Tag: "finance",
		Security: true, Roles: []string{"host", "internal_admin"},
		Responses: map[string]any{
			"200": openapi.Ref("PayoutPage"), "400": openapi.Ref("Error"),
			"401": openapi.Ref("Error"), "403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/payouts/{id}", OperationID: "getPayout",
		Summary: "Get one payout", Tag: "finance",
		Security: true, Roles: []string{"host", "internal_admin"},
		Responses: map[string]any{
			"200": openapi.Ref("Payout"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
