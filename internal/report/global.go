package report

import (
	"sync"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

var (
	globalAggregator     *Aggregator
	globalAggregatorOnce sync.Once
)

// global returns the global client report Aggregator singleton.
func global() *Aggregator {
	globalAggregatorOnce.Do(func() {
		globalAggregator = NewAggregator()
	})
	return globalAggregator
}

// SetEnabled enables or disables client report recording.
func SetEnabled(b bool) {
	global().enabled.Store(b)
}

// Record allows recording an outcome to the global Aggregator.
func Record(reason DiscardReason, category ratelimit.Category, quantity int64) {
	global().RecordOutcome(reason, category, quantity)
}

// RecordOne allows recording a single outcome to the global Aggregator.
func RecordOne(reason DiscardReason, category ratelimit.Category) {
	global().RecordOutcome(reason, category, 1)
}

// TakeReport returns the aggregated client report outcomes.
func TakeReport() *ClientReport {
	return global().TakeReport()
}
