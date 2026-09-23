// Package validation carries safe, client-facing validation details across
// domain and transport boundaries without exposing internal errors.
package validation

import "errors"

// Error wraps a domain validation sentinel with a safe field and message.
type Error struct {
	cause   error
	field   string
	message string
}

// New creates a validation error whose details may be returned to a client.
func New(cause error, field, message string) error {
	return &Error{cause: cause, field: field, message: message}
}

func (err *Error) Error() string {
	return err.message
}

func (err *Error) Unwrap() error {
	return err.cause
}

// Details returns client-safe validation details when err carries them.
func Details(err error) (field, message string, ok bool) {
	var validationError *Error
	if !errors.As(err, &validationError) {
		return "", "", false
	}
	return validationError.field, validationError.message, true
}
