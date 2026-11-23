package telemetry

import "github.com/getsentry/sentry-go/internal/ratelimit"

// Priority represents the importance level of a category for buffer management.
type Priority int

const (
	PriorityCritical Priority = iota + 1
	PriorityHigh
	PriorityMedium
	PriorityLow
	PriorityLowest
)

func (p Priority) String() string {
	switch p {
	case PriorityCritical:
		return "critical"
	case PriorityHigh:
		return "high"
	case PriorityMedium:
		return "medium"
	case PriorityLow:
		return "low"
	case PriorityLowest:
		return "lowest"
	default:
		return "unknown"
	}
}

// priorityOf returns the priority level for category.
func priorityOf(category ratelimit.Category) Priority {
	switch category {
	case ratelimit.CategoryError:
		return PriorityCritical
	case ratelimit.CategoryMonitor:
		return PriorityHigh
	case ratelimit.CategoryLog:
		return PriorityLow
	case ratelimit.CategoryTransaction:
		return PriorityMedium
	case ratelimit.CategoryTraceMetric:
		return PriorityLow
	default:
		return PriorityMedium
	}
}
