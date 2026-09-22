// Package email is the transactional email boundary (mocked for this build).
package email

import "context"

// a rendered email ready for the provider.
type Message struct {
	To          string
	TemplateKey string
	Payload     map[string]any
}

// email provider we depend on.
type Sender interface {
	Send(ctx context.Context, message Message) error
}
