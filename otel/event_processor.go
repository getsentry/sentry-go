//go:build go1.18

package sentryotel

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/trace"
)

// linkTraceContextToErrorEvent is a Sentry event processor that attaches trace information
// to the error event.
//
// Caveat: hint.Context should contain a valid context populated by OpenTelemetry's span context.
func linkTraceContextToErrorEvent(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
	if hint == nil || hint.Context == nil {
		return event
	}
	otelSpanContext := trace.SpanContextFromContext(hint.Context)
	var sentrySpan *sentry.Span
	if otelSpanContext.IsValid() {
		sentrySpan, _ = sentrySpanMap.Get(otelSpanContext.SpanID())
	}
	if sentrySpan == nil {
		return event
	}

	traceContext := event.Contexts["trace"]
	if len(traceContext) > 0 {
		// trace context is already set, not touching it
		return event
	}
	event.Contexts["trace"] = map[string]interface{}{
		"trace_id":       sentrySpan.TraceID.String(),
		"span_id":        sentrySpan.SpanID.String(),
		"parent_span_id": sentrySpan.ParentSpanID.String(),
	}
	return event
}
