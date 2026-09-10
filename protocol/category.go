package protocol

import (
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Reference:
// https://github.com/getsentry/relay/blob/46dfaa850b8717a6e22c3e9a275ba17fe673b9da/relay-base-schema/src/data_category.rs#L231-L271

// Category classifies supported payload types that can be ingested by Sentry
// and, therefore, rate limited.
type Category string

// Known rate limit categories that are specified in rate limit headers.
const (
	CategoryUnknown     Category = "unknown" // Unknown category should not get rate limited
	CategoryAll         Category = ""        // Special category for empty categories (applies to all)
	CategoryError       Category = "error"
	CategoryTransaction Category = "transaction"
	CategorySpan        Category = "span"
	CategoryLog         Category = "log_item"
	CategoryLogByte     Category = "log_byte"
	CategoryMonitor     Category = "monitor"
	CategoryTraceMetric Category = "trace_metric"
)

// String returns the category formatted for debugging.
func (c Category) String() string {
	switch c {
	case CategoryAll:
		return "CategoryAll"
	case CategoryError:
		return "CategoryError"
	case CategoryTransaction:
		return "CategoryTransaction"
	case CategorySpan:
		return "CategorySpan"
	case CategoryLog:
		return "CategoryLog"
	case CategoryLogByte:
		return "CategoryLogByte"
	case CategoryMonitor:
		return "CategoryMonitor"
	case CategoryTraceMetric:
		return "CategoryTraceMetric"
	default:
		// For unknown categories, use the original formatting logic
		caser := cases.Title(language.English)
		rv := "Category"
		for _, w := range strings.Fields(string(c)) {
			rv += caser.String(w)
		}
		return rv
	}
}
