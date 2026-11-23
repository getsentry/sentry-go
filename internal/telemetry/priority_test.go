package telemetry

import (
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

func TestPriority_String(t *testing.T) {
	tests := []struct {
		priority Priority
		expected string
	}{
		{PriorityCritical, "critical"},
		{PriorityHigh, "high"},
		{PriorityMedium, "medium"},
		{PriorityLow, "low"},
		{PriorityLowest, "lowest"},
		{Priority(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.priority.String(); got != tt.expected {
			t.Errorf("Priority(%d).String() = %q, want %q", tt.priority, got, tt.expected)
		}
	}
}

func TestPriorityOf(t *testing.T) {
	tests := []struct {
		category ratelimit.Category
		expected Priority
	}{
		{ratelimit.CategoryError, PriorityCritical},
		{ratelimit.CategoryMonitor, PriorityHigh},
		{ratelimit.CategoryLog, PriorityLow},
		{ratelimit.CategoryTransaction, PriorityMedium},
		{ratelimit.CategoryTraceMetric, PriorityLow},
		{ratelimit.Category("unknown"), PriorityMedium},
	}

	for _, tt := range tests {
		if got := priorityOf(tt.category); got != tt.expected {
			t.Errorf("priorityOf(%q) = %s, want %s", tt.category, got, tt.expected)
		}
	}
}
