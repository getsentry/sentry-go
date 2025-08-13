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
	spanMap     map[otelTrace.SpanID]*SpanEntry
	childrenMap map[sentry.SpanID][]otelTrace.SpanID
	mu          sync.RWMutex
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

	if sentrySpan.ParentSpanID != (sentry.SpanID{}) {
		ssm.childrenMap[sentrySpan.ParentSpanID] = append(ssm.childrenMap[sentrySpan.ParentSpanID], otelSpandID)
	}
}

func (ssm *SentrySpanMap) Delete(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	if entry, ok := ssm.spanMap[otelSpandID]; ok && entry != nil && entry.Span != nil {
		delete(ssm.childrenMap, entry.Span.SpanID)
	}
	delete(ssm.spanMap, otelSpandID)
}

func (ssm *SentrySpanMap) Clear() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap = make(map[otelTrace.SpanID]*SpanEntry)
	ssm.childrenMap = make(map[sentry.SpanID][]otelTrace.SpanID)
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

func (ssm *SentrySpanMap) CleanupFinishedSpan(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	entry, ok := ssm.spanMap[otelSpandID]
	if !ok || entry == nil || entry.Span == nil {
		return
	}

	sentrySpanID := entry.Span.SpanID
	parentSpanID := entry.Span.ParentSpanID

	if children, hasChildren := ssm.childrenMap[sentrySpanID]; hasChildren && len(children) > 0 {
		return
	}

	delete(ssm.childrenMap, sentrySpanID)
	delete(ssm.spanMap, otelSpandID)

	if parentSpanID != (sentry.SpanID{}) {
		if children, ok := ssm.childrenMap[parentSpanID]; ok {
			newChildren := make([]otelTrace.SpanID, 0, len(children)-1)
			for _, childID := range children {
				if childID != otelSpandID {
					newChildren = append(newChildren, childID)
				}
			}

			if len(newChildren) == 0 {
				delete(ssm.childrenMap, parentSpanID)

				for otelID, entry := range ssm.spanMap {
					if entry != nil && entry.Span != nil && entry.Span.SpanID == parentSpanID {
						if entry.Finished {
							delete(ssm.spanMap, otelID)
						}
						break
					}
				}
			} else {
				ssm.childrenMap[parentSpanID] = newChildren
			}
		}
	}
}

var sentrySpanMap = SentrySpanMap{
	spanMap:     make(map[otelTrace.SpanID]*SpanEntry),
	childrenMap: make(map[sentry.SpanID][]otelTrace.SpanID),
}
