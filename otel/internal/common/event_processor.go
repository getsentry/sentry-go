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

	propagation, ok := ResolveTraceContext(hint.Context)
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
	traceContext["trace_id"] = propagation.TraceID.String()
	traceContext["span_id"] = propagation.SpanID.String()
	return event
}

// ResolveTraceContext returns propagation state from the active OTel span in ctx.
func ResolveTraceContext(ctx context.Context) (sentry.PropagationContext, bool) {
	if ctx == nil {
		return sentry.PropagationContext{}, false
	}

	otelSpanContext := trace.SpanContextFromContext(ctx)
	if !otelSpanContext.IsValid() {
		return sentry.PropagationContext{}, false
	}

	sampled := sentry.SampledFalse
	if otelSpanContext.IsSampled() {
		sampled = sentry.SampledTrue
	}
	return sentry.PropagationContext{
		TraceID: sentry.TraceID(otelSpanContext.TraceID()),
		SpanID:  sentry.SpanID(otelSpanContext.SpanID()),
		Sampled: sampled,
		DynamicSamplingContext: sentry.DynamicSamplingContext{
			Frozen: true,
		},
	}, true
}
