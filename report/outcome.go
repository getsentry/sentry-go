package report

import (
	"github.com/getsentry/sentry-go/protocol"
)

// outcomeKey identifies an outcome bucket for aggregation.
type outcomeKey struct {
	Reason   DiscardReason
	Category protocol.DataCategory
}

// DiscardedEvent is the quantity of a category discarded for a reason.
type DiscardedEvent struct {
	Reason   DiscardReason         `json:"reason"`
	Category protocol.DataCategory `json:"category"`
	Quantity int64                 `json:"quantity"`
}
