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
	//
	// A non-nil error means the envelope was rejected: the SDK records it as
	// lost, and ErrTransportQueueFull also makes the SDK back off. Returning
	// nil means the transport accepted the envelope and accounts for any later
	// delivery loss itself.
	SendEnvelope(*protocol.Envelope) error

	// Flush waits for pending envelopes to be sent, up to the timeout.
	Flush(time.Duration) bool

	// FlushWithContext waits for pending envelopes until the context expires.
	FlushWithContext(context.Context) bool

	// Close releases resources. Flush before closing to finish pending delivery.
	Close()
}
