package sentryotel

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type sentryPropagator struct{}

// Inject set tracecontext from the Context into the carrier.
func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	fmt.Printf("\n--- Propagator Inject\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

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
		sentryTraceHeader, _ := ctx.Value(utils.SentryTraceHeaderKey()).(string)
		if sentryTraceHeader != "" {
			carrier.Set(sentry.SentryTraceHeader, sentryTraceHeader)
		}
	} else {
		// Sentry span exists => generate "sentry-trace" from it
		carrier.Set(sentry.SentryTraceHeader, sentrySpan.ToSentryTrace())
	}

	// Propagate baggage header
	sentryBaggageStr := ""
	if sentrySpan != nil && sentrySpan.IsTransaction() {
		// TODO(anton): Normally, this should return the DSC baggage from the transaction
		// (not necessarily the current span). So this might break things in some cases.
		sentryBaggageStr = sentrySpan.ToBaggage()
	}
	sentryBaggage, _ := baggage.Parse(sentryBaggageStr)

	// Merge the baggage values
	finalBaggage := baggage.FromContext(ctx)
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

	fmt.Printf("sentry-trace header: '%s'\n", carrier.Get(sentry.SentryTraceHeader))
	fmt.Printf("baggage header: '%s'\n", carrier.Get(sentry.SentryBaggageHeader))
}

// Extract reads cross-cutting concerns from the carrier into a Context.
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	fmt.Printf("\n--- Propagator Extract\nContext: %#v\nCarrier: %#v\n", ctx, carrier)

	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)
	fmt.Printf("sentry-trace header: '%s'\n", sentryTraceHeader)

	if sentryTraceHeader != "" {
		ctx = context.WithValue(ctx, utils.SentryTraceHeaderKey(), sentryTraceHeader)
		if traceParentContext, valid := sentry.ParseTraceParentContext([]byte(sentryTraceHeader)); valid {
			// Save traceParentContext because we'll at least need to know the original "sampled"
			// value in the span processor.
			ctx = context.WithValue(ctx, utils.SentryTraceParentContextKey(), traceParentContext)

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
	fmt.Printf("baggage header: '%s'\n", baggageHeader)
	if baggageHeader != "" {
		// Preserve the original baggage
		parsedBaggage, err := baggage.Parse(baggageHeader)
		if err == nil {
			ctx = baggage.ContextWithBaggage(ctx, parsedBaggage)
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

	ctx = context.WithValue(ctx, utils.DynamicSamplingContextKey(), dynamicSamplingContext)
	fmt.Printf("DSC: %#v\n", dynamicSamplingContext)

	return ctx
}

func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}

func NewSentryPropagator() propagation.TextMapPropagator {
	return sentryPropagator{}
}
