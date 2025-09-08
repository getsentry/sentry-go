package sentry

import (
	"encoding/json"
	"time"
)

// EnvelopeHeader represents the header of a Sentry envelope.
type EnvelopeHeader struct {
	// EventID is the unique identifier for this event
	EventID EventID `json:"event_id,omitempty"`

	// SentAt is the timestamp when the event was sent from the SDK as string in RFC 3339 format.
	// Used for clock drift correction of the event timestamp. The time zone must be UTC.
	SentAt time.Time `json:"sent_at,omitempty"`

	// Dsn can be used for self-authenticated envelopes.
	// This means that the envelope has all the information necessary to be sent to sentry.
	// In this case the full DSN must be stored in this key.
	Dsn string `json:"dsn,omitempty"`

	// Sdk carries the same payload as the sdk interface in the event payload but can be carried for all events.
	// This means that SDK information can be carried for minidumps, session data and other submissions.
	Sdk *SdkInfo `json:"sdk,omitempty"`

	// Trace contains trace context information for distributed tracing
	Trace map[string]string `json:"trace,omitempty"`
}

// MarshalJSON converts the EnvelopeHeader to JSON and ensures it's a single line.
func (h *EnvelopeHeader) MarshalJSON() ([]byte, error) {
	type header EnvelopeHeader
	return json.Marshal((*header)(h))
}
