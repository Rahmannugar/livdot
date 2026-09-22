package ticketing

import "github.com/Rahmannugar/livdot/internal/infra/openapi"

// RegisterContract registers the purchase and ticket routes and their hand-maintained results.
func RegisterContract(registry *openapi.Registry) {
	purchase := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string"},
			"eventId":     map[string]any{"type": "string"},
			"userId":      map[string]any{"type": "string"},
			"amountMinor": map[string]any{"type": "integer", "format": "int64"},
			"status":      map[string]any{"type": "string", "enum": []string{"initiated", "processing", "paid", "refunded", "failed"}},
			"checkoutUrl": map[string]any{"type": "string", "nullable": true},
			"ticketId":    map[string]any{"type": "string"},
			"paidAt":      map[string]any{"type": "string", "format": "date-time", "nullable": true},
			"createdAt":   map[string]any{"type": "string", "format": "date-time"},
		},
	}
	ticket := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":                   map[string]any{"type": "string"},
			"eventId":              map[string]any{"type": "string"},
			"userId":               map[string]any{"type": "string"},
			"status":               map[string]any{"type": "string", "enum": []string{"temporarily_reserved", "reservation_expired", "issued", "revoked"}},
			"reservationExpiresAt": map[string]any{"type": "string", "format": "date-time"},
			"issuedAt":             map[string]any{"type": "string", "format": "date-time", "nullable": true},
		},
	}
	registry.Add(openapi.Operation{
		Method: "post", Path: "/api/events/{id}/purchase", OperationID: "purchaseTicket",
		Summary: "Reserve a ticket and start a payment", Tag: "ticketing",
		Security: true, Roles: []string{"user"},
		Request: purchaseRequest{}, RequestName: "PurchaseTicketRequest",
		Responses: map[string]any{
			"201": purchase, "400": openapi.Ref("Error"), "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "409": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
	registry.Add(openapi.Operation{
		Method: "get", Path: "/api/tickets/{id}", OperationID: "getTicket",
		Summary: "Get a ticket the caller owns", Tag: "ticketing",
		Security: true, Roles: []string{"user"},
		Responses: map[string]any{
			"200": ticket, "401": openapi.Ref("Error"),
			"403": openapi.Ref("Error"), "404": openapi.Ref("Error"), "429": openapi.Ref("Error"),
		},
	})
}
