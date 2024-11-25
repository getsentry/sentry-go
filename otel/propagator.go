package sentryotel

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type sentryPropagator struct{}

func NewSentryPropagator() propagation.TextMapPropagator {
	return &sentryPropagator{}
}

// Inject sets Sentry-related values from the Context into the carrier.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#inject
func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	spanContext := trace.SpanContextFromContext(ctx)

	var sentrySpan *sentry.Span

	if spanContext.IsValid() {
		sentrySpan, _ = sentrySpanMap.Get(spanContext.SpanID())
	} else {
		sentrySpan = nil
	}

	// Propagate sentry-trace header
	if sentrySpan == nil {
		// No span => propagate the incoming sentry-trace header, if exists
		sentryTraceHeader, _ := ctx.Value(sentryTraceHeaderContextKey{}).(string)
		if sentryTraceHeader != "" {
			carrier.Set(sentry.SentryTraceHeader, sentryTraceHeader)
		}
	} else {
		// Sentry span exists => generate "sentry-trace" from it
		carrier.Set(sentry.SentryTraceHeader, sentrySpan.ToSentryTrace())
	}

	// Propagate baggage header
	sentryBaggageStr := ""
	if sentrySpan != nil {
		sentryBaggageStr = sentrySpan.GetTransaction().ToBaggage()
	}
	// FIXME(anton): We're basically reparsing the header again, because in sentry-go
	// we currently don't expose a method to get only DSC or its baggage (only a string).
	// This is not optimal and we should consider other approaches.
	sentryBaggage, _ := baggage.Parse(sentryBaggageStr)

	// Merge the baggage values
	finalBaggage, baggageOk := ctx.Value(baggageContextKey{}).(baggage.Baggage)
	if !baggageOk {
		finalBaggage = baggage.Baggage{}
	}
	for _, member := range sentryBaggage.Members() {
		var err error
		finalBaggage, err = finalBaggage.SetMember(member)
		if err != nil {
			continue
		}
	}

	if finalBaggage.Len() > 0 {
		carrier.Set(sentry.SentryBaggageHeader, finalBaggage.String())
	}
}

// Extract reads cross-cutting concerns from the carrier into a Context.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#extract
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)

	if sentryTraceHeader != "" {
		ctx = context.WithValue(ctx, sentryTraceHeaderContextKey{}, sentryTraceHeader)
		if traceParentContext, valid := sentry.ParseTraceParentContext([]byte(sentryTraceHeader)); valid {
			// Save traceParentContext because we'll at least need to know the original "sampled"
			// value in the span processor.
			ctx = context.WithValue(ctx, sentryTraceParentContextKey{}, traceParentContext)

			spanContextConfig := trace.SpanContextConfig{
				TraceID:    trace.TraceID(traceParentContext.TraceID),
				SpanID:     trace.SpanID(traceParentContext.ParentSpanID),
				TraceFlags: trace.FlagsSampled,
				Remote:     true,
			}
			ctx = trace.ContextWithSpanContext(ctx, trace.NewSpanContext(spanContextConfig))
		}
	}

	baggageHeader := carrier.Get(sentry.SentryBaggageHeader)
	if baggageHeader != "" {
		// Preserve the original baggage
		parsedBaggage, err := baggage.Parse(baggageHeader)
		if err == nil {
			ctx = context.WithValue(ctx, baggageContextKey{}, parsedBaggage)
		}
	}

	// The following cases should be already covered below:
	// * We can extract a valid dynamic sampling context (DSC) from the baggage
	// * No baggage header is present
	// * No Sentry-related values are present
	// * We cannot parse the baggage header for whatever reason
	dynamicSamplingContext, err := sentry.DynamicSamplingContextFromHeader([]byte(baggageHeader))
	if err != nil {
		// If there are any errors, create a new non-frozen one.
		dynamicSamplingContext = sentry.DynamicSamplingContext{Frozen: false}
	}

	ctx = context.WithValue(ctx, dynamicSamplingContextKey{}, dynamicSamplingContext)
	return ctx
}

// Fields returns a list of fields that will be used by the propagator.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#fields
func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}
