package report

import (
	"sync"
)

// registry is a global map from DSN string to its associated Aggregator.
// The Client should be the only component creating aggregators. Other components
// (transports, telemetry buffers) should only fetch existing aggregators.
var registry struct {
	mu          sync.RWMutex
	aggregators map[string]*Aggregator
}

// nolint:gochecknoinits
func init() {
	registry.aggregators = make(map[string]*Aggregator)
}

// GetAggregator returns the existing Aggregator for a DSN, or nil if none exists.
func GetAggregator(dsn string) *Aggregator {
	if dsn == "" {
		return nil
	}

	registry.mu.RLock()
	defer registry.mu.RUnlock()
	return registry.aggregators[dsn]
}

// GetOrCreateAggregator returns the existing Aggregator for a DSN, or creates a new one if none exists.
//
// Since the client is the source of truth for client reports, it should be the only one that calls this method.
// Other components should use GetAggregator, without registering a new one.
func GetOrCreateAggregator(dsn string) *Aggregator {
	if dsn == "" {
		return nil
	}

	registry.mu.RLock()
	if agg, exists := registry.aggregators[dsn]; exists {
		registry.mu.RUnlock()
		return agg
	}
	registry.mu.RUnlock()

	registry.mu.Lock()
	defer registry.mu.Unlock()

	// Double-check after acquiring write lock
	if agg, exists := registry.aggregators[dsn]; exists {
		return agg
	}

	agg := NewAggregator()
	registry.aggregators[dsn] = agg
	return agg
}

// UnregisterAggregator removes the Aggregator for a DSN from the registry.
func UnregisterAggregator(dsn string) {
	if dsn == "" {
		return
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	delete(registry.aggregators, dsn)
}

// ClearRegistry removes all registered aggregators.
func ClearRegistry() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.aggregators = make(map[string]*Aggregator)
}
