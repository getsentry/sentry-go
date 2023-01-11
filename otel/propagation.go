package sentryotel

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type sentryPropagator struct{}

// Inject set tracecontext from the Context into the carrier.
func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	fmt.Printf("\n--- Propagator Inject\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

	spanContext := trace.SpanContextFromContext(ctx)

	if !spanContext.IsValid() {
		return
	}

	// FIXME(anton): the span map should be accessible here
	// sentrySpan := SENTRY_SPAN_PROCESSOR_MAP.get(spanContext.spanId);
	sentrySpan := &sentry.Span{}
	if sentrySpan == nil {
		return
	}

	carrier.Set(sentry.SentryTraceHeader, sentrySpan.ToSentryTrace())
	// TODO(anton): this is basically the isTransaction check
	if len(sentrySpan.TraceID) > 0 {
		baggageValue := sentrySpan.ToBaggage()
		if baggageValue != "" {
			// TODO(anton): Won't this override the existing baggage header?
			carrier.Set(sentry.SentryBaggageHeader, baggageValue)
		}
	}

}

// Extract reads cross-cutting concerns from the carrier into a Context.
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	fmt.Printf("\n--- Propagator Extract\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)

	// TODO(anton): Can we return early? Or we still need to handle baggage/DS?
	if sentryTraceHeader == "" {
		return ctx
	}

	baggageHeader := carrier.Get(sentry.SentryBaggageHeader)

	// Probably not necessary to go through sentry.Span for this
	var s sentry.Span
	sentry.ContinueFromHeaders(sentryTraceHeader, baggageHeader)(&s)

	spanContextConfig := trace.SpanContextConfig{
		TraceID:    trace.TraceID(s.TraceID),
		SpanID:     trace.SpanID(s.ParentSpanID),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	}
	ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(spanContextConfig))

	// TODO(anton): Handle errors?
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
