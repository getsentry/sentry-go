package ratelimit

import "github.com/getsentry/sentry-go/protocol"

// Category and its constants retain the internal rate-limit vocabulary.
// The public protocol package owns their definitions.
type Category = protocol.Category

const (
	CategoryUnknown     = protocol.CategoryUnknown
	CategoryAll         = protocol.CategoryAll
	CategoryError       = protocol.CategoryError
	CategoryTransaction = protocol.CategoryTransaction
	CategorySpan        = protocol.CategorySpan
	CategoryLog         = protocol.CategoryLog
	CategoryLogByte     = protocol.CategoryLogByte
	CategoryMonitor     = protocol.CategoryMonitor
	CategoryTraceMetric = protocol.CategoryTraceMetric
)

// knownCategories is the set of currently known categories. Other categories
// are ignored for the purpose of rate-limiting.
var knownCategories = map[Category]struct{}{
	CategoryAll:         {},
	CategoryError:       {},
	CategoryTransaction: {},
	CategoryLog:         {},
	CategoryMonitor:     {},
	CategoryTraceMetric: {},
}

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

// PriorityForCategory returns the scheduling priority of a telemetry category.
func PriorityForCategory(c Category) Priority {
	switch c {
	case CategoryError:
		return PriorityCritical
	case CategoryMonitor:
		return PriorityHigh
	case CategoryLog:
		return PriorityLow
	case CategoryTransaction:
		return PriorityMedium
	case CategoryTraceMetric:
		return PriorityLow
	default:
		return PriorityMedium
	}
}
