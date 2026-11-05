package report

import (
	"github.com/getsentry/sentry-go/protocol"
)

// OutcomeKey uniquely identifies an outcome bucket for aggregation.
type OutcomeKey struct {
	Reason   DiscardReason
	Category protocol.DataCategory
}

// DiscardedEvent represents a single discard event outcome for the OutcomeKey.
type DiscardedEvent struct {
	Reason   DiscardReason         `json:"reason"`
	Category protocol.DataCategory `json:"category"`
	Quantity int64                 `json:"quantity"`
}
