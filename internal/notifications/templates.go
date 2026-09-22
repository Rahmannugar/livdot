package notifications

import (
	"fmt"
	"strings"
)

// a rendered email template. {{key}} placeholders come from the payload.
type template struct {
	subject string
	body    string
}

var templates = map[string]template{
	TemplateTicketReceipt: {
		subject: "Your LIV DOT ticket",
		body:    "Your ticket for event {{eventId}} is ready. Ticket ID: {{ticketId}}.",
	},
	TemplateRefundReceipt: {
		subject: "Your LIV DOT refund",
		body:    "We have refunded {{amountMinor}} for event {{eventId}}.",
	},
	TemplateCrewAssigned: {
		subject: "You have a new LIV DOT event",
		body:    "You are assigned to event {{eventId}} ({{eventName}}).",
	},
}

// Render fills a template for a payload. An unknown key falls back to a generic
// body so a missing template never blocks delivery.
func Render(key string, payload map[string]any) (subject, body string) {
	selected, ok := templates[key]
	if !ok {
		return "LIV DOT notification", fmt.Sprintf("Notification: %s", key)
	}
	return selected.subject, fill(selected.body, payload)
}

func fill(body string, payload map[string]any) string {
	for key, value := range payload {
		body = strings.ReplaceAll(body, "{{"+key+"}}", fmt.Sprintf("%v", value))
	}
	return body
}
