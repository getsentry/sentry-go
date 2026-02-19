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
	// spans holds active (not yet finished) spans for Get lookups.
	spans util.SyncMap[otelTrace.SpanID, *sentry.Span]
	// knownSpanIDs tracks all span IDs ever added to this transaction.
	knownSpanIDs util.SyncMap[otelTrace.SpanID, struct{}]
}

// HasSpan returns true if the given spanID was ever part of this transaction.
func (te *TransactionEntry) HasSpan(spanID otelTrace.SpanID) bool {
	_, ok := te.knownSpanIDs.Load(spanID)
	return ok
}

// SentrySpanMap is a mapping between OpenTelemetry spans and Sentry spans.
// It stores spans per transaction for lookup by the propagator and event processor,
// and manages transaction entries for creating child spans via the shared spanRecorder.
type SentrySpanMap struct {
	transactions util.SyncMap[otelTrace.TraceID, *TransactionEntry]
}

// Get returns the current sentry.Span associated with the given OTel traceID and spanID.
func (ssm *SentrySpanMap) Get(traceID otelTrace.TraceID, spanID otelTrace.SpanID) (*sentry.Span, bool) {
	entry, ok := ssm.transactions.Load(traceID)
	if !ok {
		return nil, false
	}
	return entry.spans.Load(spanID)
}

// GetTransaction returns the transaction information for the given OTel traceID.
func (ssm *SentrySpanMap) GetTransaction(traceID otelTrace.TraceID) (*TransactionEntry, bool) {
	return ssm.transactions.Load(traceID)
}

// Set stores the span and transaction information on the map. It handles both root and child spans automatically.
//
// If there is a cache miss on the given traceID, a transaction entry is created. Subsequent calls for the same traceID
// just increment the active span count and store the span in the entry.
func (ssm *SentrySpanMap) Set(spanID otelTrace.SpanID, span *sentry.Span, traceID otelTrace.TraceID) {
	t := &TransactionEntry{root: span}
	t.activeCount.Store(1)
	t.spans.Store(spanID, span)
	t.knownSpanIDs.Store(spanID, struct{}{})

	if existing, loaded := ssm.transactions.LoadOrStore(traceID, t); loaded {
		existing.activeCount.Add(1)
		existing.spans.Store(spanID, span)
		existing.knownSpanIDs.Store(spanID, struct{}{})
	}
}

// MarkFinished removes a span from the active set and decrements the transaction's active count.
// When the count reaches zero, the transaction entry is removed.
// The span ID is kept in knownSpanIDs so that HasSpan continues to work for child span creation.
func (ssm *SentrySpanMap) MarkFinished(spanID otelTrace.SpanID, traceID otelTrace.TraceID) {
	entry, ok := ssm.transactions.Load(traceID)
	if !ok {
		return
	}

	entry.spans.Delete(spanID)

	if entry.activeCount.Add(-1) <= 0 {
		ssm.transactions.CompareAndDelete(traceID, entry)
	}
}

// Clear removes all spans stored on the map.
func (ssm *SentrySpanMap) Clear() {
	ssm.transactions.Clear()
}

// Len returns the number of spans on the map.
func (ssm *SentrySpanMap) Len() int {
	count := 0
	ssm.transactions.Range(func(_ otelTrace.TraceID, entry *TransactionEntry) bool {
		count += int(entry.activeCount.Load())
		return true
	})
	return count
}

var sentrySpanMap = SentrySpanMap{}
