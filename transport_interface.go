package sentry

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/protocol"
)

// Transport delivers envelopes prepared by the telemetry processor.
// Implementations must be safe for concurrent use. Embed a Transport to wrap
// selected methods while forwarding the remaining operations to it.
type Transport interface {
	// SendEnvelope takes ownership of an envelope. The caller must not mutate
	// it after this call. Async implementations return after queuing; sync
	// implementations wait for the request to finish.
	// Transports enforce their own rate limits and queue capacity and account
	// for delivery losses, including reports attached to rejected envelopes.
	SendEnvelope(*protocol.Envelope) error

	// Flush waits for pending envelopes to be sent, up to the timeout.
	Flush(time.Duration) bool

	// FlushWithContext waits for pending envelopes until the context expires.
	FlushWithContext(context.Context) bool

	// Close releases resources. Flush before closing to finish pending delivery.
	Close()
}
