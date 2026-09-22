package finance

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the refund and payout routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	refund := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string"},
			"eventId":          map[string]any{"type": "string"},
			"userId":           map[string]any{"type": "string"},
			"purchaseId":       map[string]any{"type": "string"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64"},
			"status":           map[string]any{"type": "string", "enum": []string{"processing", "refunded", "failed"}},
			"providerRefundId": map[string]any{"type": "string", "nullable": true},
			"refundedAt":       map[string]any{"type": "string", "format": "date-time", "nullable": true},
		},
	}
	payout := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":               map[string]any{"type": "string"},
			"eventId":          map[string]any{"type": "string"},
			"hostId":           map[string]any{"type": "string"},
			"amountMinor":      map[string]any{"type": "integer", "format": "int64"},
			"status":           map[string]any{"type": "string", "enum": []string{"processing", "paid", "failed"}},
			"providerPayoutId": map[string]any{"type": "string", "nullable": true},
			"paidAt":           map[string]any{"type": "string", "format": "date-time", "nullable": true},
		},
	}
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/refund", OperationID: "refundEvent",
		Summary: "Refund a failed event's viewers", Tag: "finance",
		Security: true, Roles: []string{"internal_admin"},
		Responses: map[string]any{
			"202": map[string]any{
				"type":       "object",
				"properties": map[string]any{"refunds": map[string]any{"type": "integer"}},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/refunds", OperationID: "listRefunds",
		Summary: "List refunds the caller may see", Tag: "finance",
		Security: true, Roles: []string{"user", "internal_admin"},
		Responses: map[string]any{
			"200": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"refunds":    map[string]any{"type": "array", "items": refund},
					"nextCursor": map[string]any{"type": "string"},
				},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/refunds/{id}", OperationID: "getRefund",
		Summary: "Get one refund", Tag: "finance",
		Security: true, Roles: []string{"user", "internal_admin"},
		Responses: map[string]any{
			"200": refund, "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/payouts", OperationID: "listPayouts",
		Summary: "List payouts the caller may see", Tag: "finance",
		Security: true, Roles: []string{"host", "internal_admin"},
		Responses: map[string]any{
			"200": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"payouts":    map[string]any{"type": "array", "items": payout},
					"nextCursor": map[string]any{"type": "string"},
				},
			},
			"400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/payouts/{id}", OperationID: "getPayout",
		Summary: "Get one payout", Tag: "finance",
		Security: true, Roles: []string{"host", "internal_admin"},
		Responses: map[string]any{
			"200": payout, "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
