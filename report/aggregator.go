package report

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/protocol"
)

// Aggregator collects discarded event outcomes for client reports. It is safe for concurrent use, and a nil
// Aggregator records nothing.
type Aggregator struct {
	// outcomes maps outcomeKey to *atomic.Int64. Keys are never deleted, so a concurrent Record can't add to a
	// counter that is no longer in the map.
	outcomes sync.Map
}

// NewAggregator creates a new client report Aggregator.
func NewAggregator() *Aggregator {
	return &Aggregator{}
}

// Record records quantity items of category as discarded for reason.
func (a *Aggregator) Record(reason DiscardReason, category protocol.DataCategory, quantity int64) {
	if a == nil || quantity <= 0 {
		return
	}
	key := outcomeKey{Reason: reason, Category: category}
	counter, ok := a.outcomes.Load(key)
	if !ok {
		counter, _ = a.outcomes.LoadOrStore(key, new(atomic.Int64))
	}
	counter.(*atomic.Int64).Add(quantity)
}

// TakeReport atomically takes all accumulated outcomes and returns a ClientReport.
func (a *Aggregator) TakeReport() *ClientReport {
	if a == nil {
		return nil
	}

	var events []DiscardedEvent
	a.outcomes.Range(func(key, counter any) bool {
		if quantity := counter.(*atomic.Int64).Swap(0); quantity > 0 {
			outcome := key.(outcomeKey)
			events = append(events, DiscardedEvent{Reason: outcome.Reason, Category: outcome.Category, Quantity: quantity})
		}
		return true
	})
	if len(events) == 0 {
		return nil
	}

	return &ClientReport{
		Timestamp:       time.Now(),
		DiscardedEvents: events,
	}
}
