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

// SetEnabled enables or disables client report recording.
func SetEnabled(b bool) {
	global().enabled.Store(b)
}

// Record allows recording an outcome to the global aggregator.
func Record(reason DiscardReason, category ratelimit.Category, quantity int64) {
	global().RecordOutcome(reason, category, quantity)
}

// RecordOne allows recording a single outcome to the global aggregator.
func RecordOne(reason DiscardReason, category ratelimit.Category) {
	global().RecordOutcome(reason, category, 1)
}

// TakeReport returns the aggregated client report outcomes.
func TakeReport() *ClientReport {
	return global().TakeReport()
}
