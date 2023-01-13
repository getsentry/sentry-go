package sentryotel

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
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

		// baggage := baggage.FromContext(ctx)

		// Update the existing baggage with the (potentially updated) dynamic
		// sampling context.

		baggageValue := sentrySpan.ToBaggage()
		if baggageValue != "" {
			carrier.Set(sentry.SentryBaggageHeader, baggageValue)
		}
	}

}

// Extract reads cross-cutting concerns from the carrier into a Context.
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	fmt.Printf("\n--- Propagator Extract\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)

	if sentryTraceHeader != "" {
		if traceparentData, valid := sentry.ExtractSentryTrace([]byte(sentryTraceHeader)); valid {
			spanContextConfig := trace.SpanContextConfig{
				TraceID:    trace.TraceID(traceparentData.TraceID),
				SpanID:     trace.SpanID(traceparentData.ParentSpanID),
				TraceFlags: trace.FlagsSampled,
				// TODO(anton): wtf is this
				Remote: true,
			}
			ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(spanContextConfig))
		}
	}

	baggageHeader := carrier.Get(sentry.SentryBaggageHeader)
	// The following cases should be already covered here:
	// * We can extract a valid dynamic sampling context (DSC) from the baggage
	// * No baggage header is present
	// * No Sentry-related values are present
	// * We cannot parse the baggage header for whatever reason
	// In all of these cases we want to end up with a frozen DSC.
	dynamicSamplingContext, err := sentry.DynamicSamplingContextFromHeader([]byte(baggageHeader))
	if err != nil {
		// If there are any errors, create
		dynamicSamplingContext = sentry.DynamicSamplingContext{}
	}
	dynamicSamplingContext.Frozen = true
	ctx = context.WithValue(ctx, utils.DynamicSamplingContextKey(), dynamicSamplingContext)
	return ctx
}

func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}

func NewSentryPropagator() propagation.TextMapPropagator {
	return sentryPropagator{}
}
