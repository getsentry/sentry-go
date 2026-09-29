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

	// MakeSerializationSafe prevents serialization races, by serializing user mutable data on the foreground.
	// Should be used before passing telemetry to the processor.
	MakeSerializationSafe()
}

// EnvelopeConvertible represents items that can convert themselves to envelopes.
type EnvelopeConvertible interface {
	// GetCategory returns the rate limit category for this item.
	GetCategory() ratelimit.Category

	// GetEventID returns the event ID for this item.
	GetEventID() string

	// GetSdkInfo returns SDK information for the envelope header.
	GetSdkInfo() *protocol.SdkInfo

	// GetDynamicSamplingContext returns trace context for the envelope header.
	GetDynamicSamplingContext() map[string]string

	// ToEnvelope converts the item to a Sentry envelope using the provided header.
	ToEnvelope(*protocol.EnvelopeHeader) (*protocol.Envelope, error)
}

// ErrQueueFull reports that a transport rejected an envelope because its queue is full.
var ErrQueueFull = errors.New("transport queue full")

// transport is the delivery surface consumed by the processor. Public
// sentry.Transport implementations satisfy it without depending on the SDK.
// A SendEnvelope error means the envelope was rejected and the processor
// records the loss; ErrQueueFull also makes the processor back off.
type transport interface {
	SendEnvelope(*protocol.Envelope) error
	FlushWithContext(context.Context) bool
}

// synchronousTransport marks transports that deliver events during Add.
type synchronousTransport interface {
	Synchronous()
}
