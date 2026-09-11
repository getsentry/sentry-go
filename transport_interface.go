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
	SendEnvelope(*protocol.Envelope) error

	// HasCapacity reports whether an envelope appears likely to fit. It is
	// advisory; SendEnvelope can still fail if capacity changes concurrently.
	HasCapacity() bool

	// IsRateLimited checks whether a telemetry category is currently limited.
	IsRateLimited(protocol.Category) bool

	// Flush waits for pending envelopes to be sent, up to the timeout.
	Flush(time.Duration) bool

	// FlushWithContext waits for pending envelopes until the context expires.
	FlushWithContext(context.Context) bool

	// Close releases resources. Flush before closing to finish pending delivery.
	Close()
}
