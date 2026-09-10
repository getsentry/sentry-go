package sentry

import (
	"github.com/getsentry/sentry-go/protocol"
)

// Public DSN types share their identity with envelope header types.

// Dsn is used as the remote address source to client transport.
type Dsn = protocol.Dsn

// DsnParseError represents an error that occurs if a Sentry
// DSN cannot be parsed.
type DsnParseError = protocol.DsnParseError

// NewDsn creates a Dsn by parsing rawURL. Most users will never call this
// function directly. It is provided for use in custom Transport
// implementations.
func NewDsn(rawURL string) (*Dsn, error) {
	return protocol.NewDsn(rawURL)
}
