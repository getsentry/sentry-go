package sentry

import (
	"github.com/getsentry/sentry-go/protocol"
)

// Re-export protocol types to maintain public API compatibility

// Dsn is used as the remote address source to client transport.
type Dsn struct {
	protocol.Dsn
}

// DsnParseError represents an error that occurs if a Sentry
// DSN cannot be parsed.
type DsnParseError = protocol.DsnParseError

// NewDsn creates a Dsn by parsing rawURL. Most users will never call this
// function directly. It is provided for use in custom Transport
// implementations.
func NewDsn(rawURL string) (*Dsn, error) {
	protocolDsn, err := protocol.NewDsn(rawURL)
	if err != nil {
		return nil, err
	}
	return &Dsn{Dsn: *protocolDsn}, nil
}
