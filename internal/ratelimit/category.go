package ratelimit

import "github.com/getsentry/sentry-go/protocol"

// Category is the data category a rate limit applies to.
type Category = protocol.DataCategory

const (
	CategoryUnknown Category = "unknown" // Unknown category should not get rate limited
	CategoryAll     Category = ""        // Special category for empty categories (applies to all)

	CategoryError           = protocol.DataCategoryError
	CategoryTransaction     = protocol.DataCategoryTransaction
	CategorySpan            = protocol.DataCategorySpan
	CategoryLog             = protocol.DataCategoryLogItem
	CategoryLogByte         = protocol.DataCategoryLogByte
	CategoryMonitor         = protocol.DataCategoryMonitor
	CategoryTraceMetric     = protocol.DataCategoryTraceMetric
	CategoryTraceMetricByte = protocol.DataCategoryTraceMetricByte
	CategoryAttachment      = protocol.DataCategoryAttachment
)

// Indices into CountCategories.
const (
	CountError = iota
	CountTransaction
	CountSpan // including the transaction itself
	CountMonitor
	CountLog
	CountLogByte
	CountTraceMetric
	CountTraceMetricByte
	CountAttachment // bytes
	countCategories
)

// CountCategories lists the categories that client reports count.
var CountCategories = [countCategories]Category{
	CountError:           CategoryError,
	CountTransaction:     CategoryTransaction,
	CountSpan:            CategorySpan,
	CountMonitor:         CategoryMonitor,
	CountLog:             CategoryLog,
	CountLogByte:         CategoryLogByte,
	CountTraceMetric:     CategoryTraceMetric,
	CountTraceMetricByte: CategoryTraceMetricByte,
	CountAttachment:      CategoryAttachment,
}

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
