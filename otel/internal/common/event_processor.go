package common

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/trace"
)

// NewEventProcessor creates a Sentry event processor that attaches OTel trace
// information from the active SpanContext to an error event.
func NewEventProcessor() sentry.EventProcessor {
	return linkTraceContextToErrorEvent
}

func linkTraceContextToErrorEvent(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	if hint == nil || hint.Context == nil {
		return event
	}
	if event.Type == "transaction" {
		return event
	}

	otelSpanContext := trace.SpanContextFromContext(hint.Context)
	if !otelSpanContext.IsValid() {
		return event
	}

	if event.Contexts == nil {
		event.Contexts = make(map[string]map[string]any)
	}
	traceContext, found := event.Contexts["trace"]
	if !found || traceContext == nil {
		traceContext = make(map[string]any)
		event.Contexts["trace"] = traceContext
	}
	traceContext["trace_id"] = otelSpanContext.TraceID().String()
	traceContext["span_id"] = otelSpanContext.SpanID().String()
	return event
}
