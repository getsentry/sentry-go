package clientreport

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// Aggregator collects discarded event outcomes for client reports.
// Uses atomic operations to be safe for concurrent use.
type Aggregator struct {
	mu       sync.Mutex
	outcomes map[OutcomeKey]*atomic.Int64

	enabled atomic.Bool
}

// NewAggregator creates a new client report aggregator.
func NewAggregator() *Aggregator {
	a := &Aggregator{
		outcomes: make(map[OutcomeKey]*atomic.Int64),
	}
	a.enabled.Store(true)
	return a
}

// SetEnabled enables or disables outcome recording.
func (a *Aggregator) SetEnabled(enabled bool) {
	a.enabled.Store(enabled)
}

// IsEnabled returns whether outcome recording is enabled.
func (a *Aggregator) IsEnabled() bool {
	return a.enabled.Load()
}

// RecordOutcome records a discarded event outcome.
func (a *Aggregator) RecordOutcome(reason DiscardReason, category ratelimit.Category, quantity int64) {
	if !a.enabled.Load() || quantity <= 0 {
		return
	}

	key := OutcomeKey{Reason: reason, Category: category}

	a.mu.Lock()
	counter, exists := a.outcomes[key]
	if !exists {
		counter = &atomic.Int64{}
		a.outcomes[key] = counter
	}
	a.mu.Unlock()

	counter.Add(quantity)
}

// TakeReport atomically takes all accumulated outcomes and returns a ClientReport.
func (a *Aggregator) TakeReport() *ClientReport {
	a.mu.Lock()
	defer a.mu.Unlock()

	if len(a.outcomes) == 0 {
		return nil
	}

	var events []DiscardedEvent
	for key, counter := range a.outcomes {
		quantity := counter.Swap(0)
		if quantity > 0 {
			events = append(events, DiscardedEvent{
				Reason:   key.Reason,
				Category: key.Category,
				Quantity: quantity,
			})
		}
	}

	// Clear empty counters to prevent unbounded growth
	for key, counter := range a.outcomes {
		if counter.Load() == 0 {
			delete(a.outcomes, key)
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
