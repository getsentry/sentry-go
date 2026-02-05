package clientreport

import (
	"sync"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

var (
	globalAggregator     *Aggregator
	globalAggregatorOnce sync.Once
)

// Global returns the global client report aggregator singleton.
func Global() *Aggregator {
	globalAggregatorOnce.Do(func() {
		globalAggregator = NewAggregator()
	})
	return globalAggregator
}

// Record is a convenience function for recording an outcome to the global aggregator.
func Record(reason DiscardReason, category ratelimit.Category, quantity int64) {
	Global().RecordOutcome(reason, category, quantity)
}

// RecordOne is a convenience function for recording a single outcome to the global aggregator.
func RecordOne(reason DiscardReason, category ratelimit.Category) {
	Global().RecordOutcome(reason, category, 1)
}

// TakeReport returns a client report for sending.
func TakeReport() *ClientReport {
	return Global().TakeReport()
}
