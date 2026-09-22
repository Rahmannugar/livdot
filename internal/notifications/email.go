package notifications

// template keys shared by the emit constructors and the registry.
const (
	TemplateTicketReceipt = "ticket_receipt"
	TemplateRefundReceipt = "refund_receipt"
	TemplateCrewAssigned  = "crew_assigned"
)

// TicketIssued builds the receipt queued when a paid purchase issues a ticket.
func TicketIssued(recipientEmail, recipientAccountID, eventID, ticketID string) Email {
	return Email{
		RecipientEmail:     recipientEmail,
		RecipientAccountID: recipientAccountID,
		Type:               "ticket_issued",
		TemplateKey:        TemplateTicketReceipt,
		IdempotencyKey:     "ticket.issued:" + ticketID,
		Data: map[string]any{
			"eventId":  eventID,
			"ticketId": ticketID,
		},
	}
}

// RefundCompleted builds the receipt queued when a refund settles.
func RefundCompleted(recipientEmail, recipientAccountID, eventID, refundID string, amountMinor int64) Email {
	return Email{
		RecipientEmail:     recipientEmail,
		RecipientAccountID: recipientAccountID,
		Type:               "refund_completed",
		TemplateKey:        TemplateRefundReceipt,
		IdempotencyKey:     "refund.completed:" + refundID,
		Data: map[string]any{
			"eventId":     eventID,
			"amountMinor": amountMinor,
		},
	}
}

// CrewAssigned builds the notice queued when a host assigns a crew to an event.
func CrewAssigned(recipientEmail, recipientAccountID, eventID, eventName string) Email {
	return Email{
		RecipientEmail:     recipientEmail,
		RecipientAccountID: recipientAccountID,
		Type:               "crew_assigned",
		TemplateKey:        TemplateCrewAssigned,
		IdempotencyKey:     "event.assigned:" + eventID,
		Data: map[string]any{
			"eventId":   eventID,
			"eventName": eventName,
		},
	}
}
