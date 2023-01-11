package sentryotel

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type sentryPropagator struct{}

func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	fmt.Printf("\n--- Inject\nContext: %#v\nCarrier: %#v\n", ctx, carrier)
}

// Extract reads cross-cutting concerns from the carrier into a Context.
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	fmt.Printf("\n--- Extract\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)
	baggageHeader := carrier.Get(sentry.SentryBaggageHeader)

	if sentryTraceHeader != "" {
		var s sentry.Span
		sentry.ContinueFromHeaders(sentryTraceHeader, baggageHeader)(&s)

		spanContextConfig := trace.SpanContextConfig{
			TraceID:    trace.TraceID(s.TraceID),
			SpanID:     trace.SpanID(s.ParentSpanID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		}
		ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(spanContextConfig))
	}

	// TODO(anton): Handle errors
	// dynamicSamplingContext, _ := sentry.DynamicSamplingContextFromHeader([]byte(baggageHeader))
	// ctx = context.WithValue(ctx, sentry.DynamicSamplingContextKey, dynamicSamplingContext)

	return ctx
}

func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}

func NewSentryPropagator() propagation.TextMapPropagator {
	return sentryPropagator{}
}
