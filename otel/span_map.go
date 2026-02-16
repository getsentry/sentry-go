package sentryotel

import (
	"sync/atomic"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/util"
	otelTrace "go.opentelemetry.io/otel/trace"
)

// TransactionEntry holds a reference to the root transaction span and
// tracks the number of active spans belonging to this trace.
type TransactionEntry struct {
	root        *sentry.Span
	activeCount atomic.Int64
}

// SentrySpanMap is a mapping between OpenTelemetry spans and Sentry spans.
// It stores spans for lookup by the propagator and event processor, and
// manages transaction entries for creating child spans via the shared spanRecorder.
type SentrySpanMap struct {
	spans        util.SyncMap[otelTrace.SpanID, *sentry.Span]
	transactions util.SyncMap[otelTrace.TraceID, *TransactionEntry]
}

// Get returns the current sentry.Span associated with the given OTel spanID.
func (ssm *SentrySpanMap) Get(spanID otelTrace.SpanID) (*sentry.Span, bool) {
	return ssm.spans.Load(spanID)
}

// GetTransaction returns the transaction information for the given OTel traceID.
func (ssm *SentrySpanMap) GetTransaction(traceID otelTrace.TraceID) (*TransactionEntry, bool) {
	return ssm.transactions.Load(traceID)
}

// Set stores the span and transaction information on the map. It handles both root and child spans automatically.
//
// If there is a cache miss on the given traceID, a transaction entry is created. Subsequent calls for the same traceID
// just increment the active span count.
func (ssm *SentrySpanMap) Set(spanID otelTrace.SpanID, span *sentry.Span, traceID otelTrace.TraceID) {
	ssm.spans.Store(spanID, span)

	t := &TransactionEntry{root: span}
	t.activeCount.Store(1)

	if existing, loaded := ssm.transactions.LoadOrStore(traceID, t); loaded {
		existing.activeCount.Add(1)
	}
}

// MarkFinished removes a span from the map and decrements the transaction's active count. When the count reaches zero,
// the transaction entry is removed.
func (ssm *SentrySpanMap) MarkFinished(spanID otelTrace.SpanID, traceID otelTrace.TraceID) {
	ssm.spans.Delete(spanID)

	if entry, ok := ssm.transactions.Load(traceID); ok {
		if entry.activeCount.Add(-1) <= 0 {
			ssm.transactions.Delete(traceID)
		}
	}
}

// Clear removes all spans stored on the map.
func (ssm *SentrySpanMap) Clear() {
	ssm.spans.Clear()
	ssm.transactions.Clear()
}

// Len returns the number of spans on the map.
func (ssm *SentrySpanMap) Len() int {
	count := 0
	ssm.spans.Range(func(_ otelTrace.SpanID, _ *sentry.Span) bool {
		count++
		return true
	})
	return count
}

var sentrySpanMap = SentrySpanMap{}
