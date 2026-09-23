package ticketing

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the purchase and ticket routes and their results.
func RegisterContract(registry *openapi.Registry) {
	registry.Component("Purchase", map[string]any{
		"type": "object",
		"required": []string{
			"id", "eventId", "userId", "amountMinor", "status", "ticketId", "createdAt",
		},
		"properties": map[string]any{
			"id":                map[string]any{"type": "string", "example": "018f2c1e-aaaa-7c3b-9d4e-2b8a1c5f7e90"},
			"eventId":           map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"userId":            map[string]any{"type": "string", "example": "018f2c1e-bbbb-7c3b-9d4e-2b8a1c5f7e90"},
			"amountMinor":       map[string]any{"type": "integer", "format": "int64", "example": 500000},
			"status":            map[string]any{"type": "string", "enum": []string{"initiated", "processing", "paid", "refunded", "failed"}, "example": "initiated"},
			"checkoutUrl":       map[string]any{"type": "string", "nullable": true, "example": "https://mock.paystack.local/checkout/018f2c1e-aaaa-7c3b-9d4e-2b8a1c5f7e90"},
			"checkoutExpiresAt": map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": "2026-09-23T18:08:00Z"},
			"ticketId":          map[string]any{"type": "string", "example": "018f2c1e-cccc-7c3b-9d4e-2b8a1c5f7e90"},
			"paidAt":            map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": nil},
			"createdAt":         map[string]any{"type": "string", "format": "date-time", "example": "2026-09-23T17:58:00Z"},
		},
	})
	registry.Component("Ticket", map[string]any{
		"type": "object",
		"required": []string{
			"id", "eventId", "userId", "status", "reservationExpiresAt",
		},
		"properties": map[string]any{
			"id":                   map[string]any{"type": "string", "example": "018f2c1e-cccc-7c3b-9d4e-2b8a1c5f7e90"},
			"eventId":              map[string]any{"type": "string", "example": "018f2c1e-9a11-7c3b-9d4e-2b8a1c5f7e90"},
			"userId":               map[string]any{"type": "string", "example": "018f2c1e-bbbb-7c3b-9d4e-2b8a1c5f7e90"},
			"status":               map[string]any{"type": "string", "enum": []string{"temporarily_reserved", "reservation_expired", "issued", "revoked"}, "example": "issued"},
			"reservationExpiresAt": map[string]any{"type": "string", "format": "date-time", "example": "2026-09-23T18:08:00Z"},
			"issuedAt":             map[string]any{"type": "string", "format": "date-time", "nullable": true, "example": "2026-09-23T18:01:00Z"},
		},
	})

	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/purchase", OperationID: "purchaseTicket",
		Summary: "Reserve a ticket and start a payment", Tag: "ticketing",
		Security: true, Roles: []string{"user"},
		Request: purchaseRequest{}, RequestName: "PurchaseTicketRequest",
		Responses: map[string]any{
			"201": openapi.Ref("Purchase"), "400": openapi.Ref("Error"),
			"401": openapi.Ref("Error"), "403": openapi.Ref("Error"),
			"409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/tickets/{id}", OperationID: "getTicket",
		Summary: "Get a ticket", Tag: "ticketing",
		Security: true, Roles: []string{"user"},
		Responses: map[string]any{
			"200": openapi.Ref("Ticket"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
