package sentryotel

import (
	"sync"

	"github.com/getsentry/sentry-go"
	otelTrace "go.opentelemetry.io/otel/trace"
)

// SpanEntry represents a span in the map along with its state
type SpanEntry struct {
	Span     *sentry.Span
	Finished bool
}

// SentrySpanMap is a mapping between OpenTelemetry spans and Sentry spans.
// It helps Sentry span processor and propagator to keep track of unfinished
// Sentry spans and to establish parent-child links between spans.
type SentrySpanMap struct {
	spanMap map[otelTrace.SpanID]*SpanEntry
	mu      sync.RWMutex
}

func (ssm *SentrySpanMap) Get(otelSpandID otelTrace.SpanID) (*sentry.Span, bool) {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	entry, ok := ssm.spanMap[otelSpandID]
	if !ok || entry == nil {
		return nil, false
	}
	return entry.Span, ok
}

func (ssm *SentrySpanMap) Set(otelSpandID otelTrace.SpanID, sentrySpan *sentry.Span) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap[otelSpandID] = &SpanEntry{
		Span:     sentrySpan,
		Finished: false,
	}
}

func (ssm *SentrySpanMap) Delete(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	delete(ssm.spanMap, otelSpandID)
}

func (ssm *SentrySpanMap) Clear() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap = make(map[otelTrace.SpanID]*SpanEntry)
}

func (ssm *SentrySpanMap) Len() int {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	return len(ssm.spanMap)
}

func (ssm *SentrySpanMap) MarkFinished(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	if entry, ok := ssm.spanMap[otelSpandID]; ok && entry != nil {
		entry.Finished = true
	}
}

func (ssm *SentrySpanMap) IsFinished(otelSpandID otelTrace.SpanID) bool {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	if entry, ok := ssm.spanMap[otelSpandID]; ok && entry != nil {
		return entry.Finished
	}
	return false
}

func (ssm *SentrySpanMap) CleanupFinishedChildrenOf(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	parentEntry, ok := ssm.spanMap[otelSpandID]
	if !ok || parentEntry == nil {
		return
	}

	parentSpan := parentEntry.Span

	for spanID, entry := range ssm.spanMap {
		if entry != nil && entry.Span != nil && entry.Finished {
			if entry.Span.ParentSpanID == parentSpan.SpanID {
				delete(ssm.spanMap, spanID)
			}
		}
	}

	delete(ssm.spanMap, otelSpandID)
}

var sentrySpanMap = SentrySpanMap{spanMap: make(map[otelTrace.SpanID]*SpanEntry)}
