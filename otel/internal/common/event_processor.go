package common

import (
	"context"

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

	traceID, spanID, ok := ResolveTraceContext(hint.Context)
	if !ok {
		return event
	}

	if event.Contexts == nil {
		event.Contexts = make(map[string]sentry.Context)
	}

	traceContext, found := event.Contexts["trace"]
	if !found {
		event.Contexts["trace"] = make(map[string]any)
		traceContext = event.Contexts["trace"]
	}
	traceContext["trace_id"] = traceID.String()
	traceContext["span_id"] = spanID.String()
	return event
}

// ResolveTraceContext returns Sentry trace/span IDs from the active OTel span in ctx.
func ResolveTraceContext(ctx context.Context) (sentry.TraceID, sentry.SpanID, bool) {
	if ctx == nil {
		return sentry.TraceID{}, sentry.SpanID{}, false
	}

	otelSpanContext := trace.SpanContextFromContext(ctx)
	if !otelSpanContext.IsValid() {
		return sentry.TraceID{}, sentry.SpanID{}, false
	}

	return sentry.TraceID(otelSpanContext.TraceID()), sentry.SpanID(otelSpanContext.SpanID()), true
}
