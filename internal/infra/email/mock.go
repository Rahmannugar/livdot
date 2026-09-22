package email

import (
	"context"
	"fmt"
	"strings"
)

// no-op sender standing in for the provider.
type Mock struct{}

func NewMock() *Mock { return &Mock{} }

func (mock *Mock) Send(_ context.Context, message Message) error {
	if strings.TrimSpace(message.To) == "" {
		return fmt.Errorf("email recipient is required")
	}
	if strings.TrimSpace(message.TemplateKey) == "" {
		return fmt.Errorf("email template key is required")
	}
	return nil
}
