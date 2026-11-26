package telemetry

import (
	"context"
	"errors"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
)

// Item represents any telemetry data that can be stored in buffers.
type Item interface {
	// GetCategory returns the rate limit category for this item.
	GetCategory() ratelimit.Category

	// DiscardCounts returns client report counts for a dropped item.
	DiscardCounts() DiscardCounts
}

// EnvelopeConvertible represents items that can convert themselves to envelopes.
type EnvelopeConvertible interface {
	// ToEnvelope builds the item's envelope, setting the header fields the item knows.
	ToEnvelope() (*protocol.Envelope, error)

	// DiscardCounts returns client report counts for a dropped item.
	DiscardCounts() DiscardCounts
}

// ErrQueueFull reports that a transport rejected an envelope because its queue is full.
var ErrQueueFull = errors.New("transport queue full")

// Transport represents the envelope-first transport interface.
// This interface is designed for the telemetry buffer system and provides
// sends with backpressure signals.
type Transport interface {
	// SendEnvelope sends an envelope to Sentry. Returns an error only if
	// the envelope is rejected.
	SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error

	// FlushWithContext waits for all pending envelopes to be sent.
	FlushWithContext(ctx context.Context) bool

	// Close shuts down the transport gracefully
	Close()
}
