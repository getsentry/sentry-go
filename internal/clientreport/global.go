package clientreport

import (
	"sync"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

var (
	globalAggregator     *aggregator
	globalAggregatorOnce sync.Once
)

// global returns the global client report aggregator singleton.
func global() *aggregator {
	globalAggregatorOnce.Do(func() {
		globalAggregator = newAggregator()
	})
	return globalAggregator
}

// Record is a convenience function for recording an outcome to the global aggregator.
func Record(reason DiscardReason, category ratelimit.Category, quantity int64) {
	global().RecordOutcome(reason, category, quantity)
}

// RecordOne is a convenience function for recording a single outcome to the global aggregator.
func RecordOne(reason DiscardReason, category ratelimit.Category) {
	global().RecordOutcome(reason, category, 1)
}

// TakeReport returns a client report for sending.
func TakeReport() *ClientReport {
	return global().TakeReport()
}
