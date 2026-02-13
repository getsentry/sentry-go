package report

import (
	"testing"
)

func TestRegistry_SharedAcrossComponents(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	dsn := "https://public@example.com/1"

	clientAgg := GetOrCreateAggregator(dsn)
	transportAgg := GetOrCreateAggregator(dsn)
	telemetryAgg := GetOrCreateAggregator(dsn)

	if clientAgg != transportAgg {
		t.Errorf("client and transport should share aggregator")
	}
	if clientAgg != telemetryAgg {
		t.Errorf("client and telemetry should share aggregator")
	}

	clientAgg.RecordOne(ReasonQueueOverflow, "error")
	transportAgg.RecordOne(ReasonRateLimitBackoff, "transaction")

	report := telemetryAgg.TakeReport()
	if report == nil {
		t.Fatal("expected report from shared aggregator")
	}

	if len(report.DiscardedEvents) != 2 {
		t.Errorf("expected 2 discarded events, got %d", len(report.DiscardedEvents))
	}
}

func TestUnregisterAggregator(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	dsn := "https://public@example.com/1"
	agg1 := GetOrCreateAggregator(dsn)
	if agg1 == nil {
		t.Fatal("expected aggregator, got nil")
	}

	UnregisterAggregator(dsn)

	agg2 := GetOrCreateAggregator(dsn)
	if agg2 == nil {
		t.Fatal("expected new aggregator after unregister, got nil")
	}
	if agg1 == agg2 {
		t.Errorf("expected different aggregator instance after unregister")
	}

	UnregisterAggregator("")
}

func TestClearRegistry(t *testing.T) {
	ClearRegistry()
	defer ClearRegistry()

	dsn1 := "https://public@example.com/1"
	dsn2 := "https://public@example.com/2"

	agg1 := GetOrCreateAggregator(dsn1)
	agg2 := GetOrCreateAggregator(dsn2)

	ClearRegistry()

	// After clear, should create new instances
	newAgg1 := GetOrCreateAggregator(dsn1)
	newAgg2 := GetOrCreateAggregator(dsn2)

	if agg1 == newAgg1 {
		t.Errorf("expected different aggregator for dsn1 after clear")
	}
	if agg2 == newAgg2 {
		t.Errorf("expected different aggregator for dsn2 after clear")
	}
}
