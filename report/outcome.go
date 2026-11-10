package report

import (
	"github.com/getsentry/sentry-go/protocol"
)

// DiscardedEvent is the quantity of a category discarded for a reason.
type DiscardedEvent struct {
	Reason   DiscardReason         `json:"reason"`
	Category protocol.DataCategory `json:"category"`
	Quantity uint64                `json:"quantity"`
}
