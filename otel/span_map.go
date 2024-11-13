package sentryotel

import (
	"sync"

	"github.com/getsentry/sentry-go"
	otelTrace "go.opentelemetry.io/otel/trace"
)

// SentrySpanMap is a mapping between OpenTelemetry spans and Sentry spans.
// It helps Sentry span processor and propagator to keep track of unfinished
// Sentry spans and to establish parent-child links between spans.
type SentrySpanMap struct {
	spanMap map[otelTrace.SpanID]*sentry.Span
	mu      sync.RWMutex
}

func (ssm *SentrySpanMap) Get(otelSpandID otelTrace.SpanID) (*sentry.Span, bool) {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	span, ok := ssm.spanMap[otelSpandID]
	return span, ok
}

func (ssm *SentrySpanMap) Set(otelSpandID otelTrace.SpanID, sentrySpan *sentry.Span) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap[otelSpandID] = sentrySpan
}

func (ssm *SentrySpanMap) Delete(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	delete(ssm.spanMap, otelSpandID)
}

func (ssm *SentrySpanMap) Clear() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap = make(map[otelTrace.SpanID]*sentry.Span)
}

func (ssm *SentrySpanMap) Len() int {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	return len(ssm.spanMap)
}

var sentrySpanMap = SentrySpanMap{spanMap: make(map[otelTrace.SpanID]*sentry.Span)}
