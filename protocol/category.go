package protocol

// DataCategory classifies data for rate limits and client reports.
type DataCategory string

// Data categories used by the SDK. See
// https://github.com/getsentry/relay/blob/46dfaa850b8717a6e22c3e9a275ba17fe673b9da/relay-base-schema/src/data_category.rs#L231-L271
const (
	DataCategoryError       DataCategory = "error"
	DataCategoryTransaction DataCategory = "transaction"
	DataCategorySpan        DataCategory = "span"
	DataCategoryLogItem     DataCategory = "log_item"
	DataCategoryLogByte     DataCategory = "log_byte" // bytes
	DataCategoryMonitor     DataCategory = "monitor"  // check-ins
	DataCategoryTraceMetric DataCategory = "trace_metric"
)
