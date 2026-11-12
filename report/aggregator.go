package report

import (
	"slices"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
)

var reasons = [...]DiscardReason{
	ReasonQueueOverflow,
	ReasonBufferOverflow,
	ReasonRateLimitBackoff,
	ReasonBeforeSend,
	ReasonEventProcessor,
	ReasonSampleRate,
	ReasonNetworkError,
	ReasonSendError,
	ReasonInternalError,
}

// Aggregator collects discarded event outcomes for client reports. It is safe for concurrent use, and a nil
// Aggregator records nothing.
type Aggregator struct {
	// outcomes is indexed by the positions in reasons and ratelimit.CountCategories.
	outcomes [len(reasons)][len(ratelimit.CountCategories)]atomic.Uint64
}

// NewAggregator creates a new client report Aggregator.
func NewAggregator() *Aggregator {
	return &Aggregator{}
}

// Record records quantity items of category as discarded for reason.
func (a *Aggregator) Record(reason DiscardReason, category protocol.DataCategory, quantity uint64) {
	if a == nil || quantity == 0 {
		return
	}
	r, c := slices.Index(reasons[:], reason), slices.Index(ratelimit.CountCategories[:], category)
	if r < 0 || c < 0 {
		return
	}
	a.outcomes[r][c].Add(quantity)
}

// TakeReport atomically takes all accumulated outcomes and returns a ClientReport.
func (a *Aggregator) TakeReport() *ClientReport {
	if a == nil {
		return nil
	}

	var events []DiscardedEvent
	for r := range a.outcomes {
		for c := range a.outcomes[r] {
			if quantity := a.outcomes[r][c].Swap(0); quantity > 0 {
				events = append(events, DiscardedEvent{Reason: reasons[r], Category: ratelimit.CountCategories[c], Quantity: quantity})
			}
		}
	}
	if len(events) == 0 {
		return nil
	}

	return &ClientReport{
		Timestamp:       time.Now(),
		DiscardedEvents: events,
	}
}
