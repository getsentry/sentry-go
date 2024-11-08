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
	// TODO: what we want here is to compare with the (unexported) sentry.transactionType
	if event.Type == "transaction" {
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

	traceContext, found := event.Contexts["trace"]
	if !found {
		event.Contexts["trace"] = make(map[string]interface{})
		traceContext = event.Contexts["trace"]
	}
	traceContext["trace_id"] = sentrySpan.TraceID.String()
	traceContext["span_id"] = sentrySpan.SpanID.String()
	traceContext["parent_span_id"] = sentrySpan.ParentSpanID.String()
	return event
}
