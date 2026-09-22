package email

import (
	"context"
	"log/slog"
)

// LogSender records emails instead of sending them. It stands in for the
// provider so the async notification path is observable in development.
type LogSender struct{}

func NewLogSender() *LogSender { return &LogSender{} }

func (sender *LogSender) Send(_ context.Context, message Message) error {
	slog.Info("mock email delivered",
		"component", "email",
		"to", message.To,
		"template", message.TemplateKey,
		"subject", message.Subject,
	)
	return nil
}
