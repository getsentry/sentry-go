package sentryotel

import (
	"sync"

	"github.com/getsentry/sentry-go"
	otelTrace "go.opentelemetry.io/otel/trace"
)

type spanInfo struct {
	span     *sentry.Span
	finished bool
	children map[otelTrace.SpanID]struct{}
	parentID otelTrace.SpanID
}

// SentrySpanMap is a mapping between OpenTelemetry spans and Sentry spans.
// It helps Sentry span processor and propagator to keep track of unfinished
// Sentry spans and to establish parent-child links between spans.
type SentrySpanMap struct {
	spanMap map[otelTrace.SpanID]*spanInfo
	mu      sync.RWMutex
}

func (ssm *SentrySpanMap) Get(otelSpandID otelTrace.SpanID) (*sentry.Span, bool) {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	info, ok := ssm.spanMap[otelSpandID]
	if !ok {
		return nil, false
	}
	return info.span, true
}

func (ssm *SentrySpanMap) Set(otelSpandID otelTrace.SpanID, sentrySpan *sentry.Span, parentID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	info := &spanInfo{
		span:     sentrySpan,
		finished: false,
		children: make(map[otelTrace.SpanID]struct{}),
		parentID: parentID,
	}
	ssm.spanMap[otelSpandID] = info

	if parentID != (otelTrace.SpanID{}) {
		if parentInfo, ok := ssm.spanMap[parentID]; ok {
			parentInfo.children[otelSpandID] = struct{}{}
		}
	}
}

func (ssm *SentrySpanMap) MarkFinished(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	info, ok := ssm.spanMap[otelSpandID]
	if !ok {
		return
	}

	info.finished = true
	ssm.tryCleanupSpan(otelSpandID)
}

// tryCleanupSpan deletes a parent and all children only if the whole subtree is marked finished.
// Must be called with lock held.
func (ssm *SentrySpanMap) tryCleanupSpan(spanID otelTrace.SpanID) {
	info, ok := ssm.spanMap[spanID]
	if !ok || !info.finished {
		return
	}

	if !info.span.IsTransaction() {
		parentID := info.parentID
		if parentID != (otelTrace.SpanID{}) {
			if parentInfo, parentExists := ssm.spanMap[parentID]; parentExists && !parentInfo.finished {
				return
			}
		}
	}

	// We need to have a lookup first to see if every child is marked as finished to actually cleanup everything.
	// There probably is a better way to do this
	for childID := range info.children {
		if childInfo, exists := ssm.spanMap[childID]; exists && !childInfo.finished {
			return
		}
	}

	parentID := info.parentID
	if parentID != (otelTrace.SpanID{}) {
		if parentInfo, ok := ssm.spanMap[parentID]; ok {
			delete(parentInfo.children, spanID)
		}
	}

	for childID := range info.children {
		if childInfo, exists := ssm.spanMap[childID]; exists && childInfo.finished {
			ssm.tryCleanupSpan(childID)
		}
	}

	delete(ssm.spanMap, spanID)
	if parentID != (otelTrace.SpanID{}) {
		ssm.tryCleanupSpan(parentID)
	}
}

func (ssm *SentrySpanMap) Clear() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap = make(map[otelTrace.SpanID]*spanInfo)
}

func (ssm *SentrySpanMap) Len() int {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	return len(ssm.spanMap)
}

var sentrySpanMap = SentrySpanMap{spanMap: make(map[otelTrace.SpanID]*spanInfo)}
