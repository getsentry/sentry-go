package sentryotel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"github.com/getsentry/sentry-go/otel/internal/common"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const sentryPrefix = "sentry-"

// Option applies configuration to the propagator.
type Option func(*sentryPropagator)

// WithDSCSource configures the propagator to look up the DSC from the given source.
// This is used for configuring the propagator to work with either OTLP or the span processor.
//
// The propagator fallbacks to a noop implementation if not provided.
func WithDSCSource(src common.DSCSource) Option {
	return func(p *sentryPropagator) {
		p.dscSource = src
	}
}

type sentryPropagator struct {
	dscSource common.DSCSource
}

// NewSentryPropagator creates a new Sentry propagator.
//
// Without options, it behaves as a lightweight propagator suitable for OTLP:
// it generates sentry-trace from the OTel SpanContext and only forwards
// baggage that was received from upstream (Extract).
//
// With WithDSCSource, it also emits baggage at the trace origin by looking up
// DSC from the provided source (e.g. the bridge span map).
func NewSentryPropagator(opts ...Option) propagation.TextMapPropagator {
	p := &sentryPropagator{dscSource: &sentrySpanMap}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Inject sets Sentry-related values from the Context into the carrier.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#inject
func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	spanContext := trace.SpanContextFromContext(ctx)
	sampled := spanContext.IsSampled()
	if !spanContext.IsValid() {
		if h := common.TraceHeaderFromContext(ctx); h != "" {
			carrier.Set(sentry.SentryTraceHeader, h)
		}
		if b := common.BaggageFromContext(ctx); b.Len() > 0 {
			carrier.Set(sentry.SentryBaggageHeader, b.String())
		}
		return
	}

	// dscSource should have priority here. It is used for specifying a more "complete" DSC to get.
	// when using the otel span processor, the DSC originates from the span map and includes span specific info.
	// Then the priority should be:
	// 1. span map (more specific)
	// 2. upstream propagation for otlp
	// 3. no trace to propagate - skip baggage
	dsc, _ := p.dscSource.GetDSC(spanContext.TraceID(), spanContext.SpanID())
	if !dsc.HasEntries() {
		dsc = common.DynamicSamplingContextFromContext(ctx)
	}
	// try and ovewrite the sampling decision from the dsc before setting the trace
	if s, err := strconv.ParseBool(dsc.Entries["sampled"]); err == nil {
		sampled = s
	}
	carrier.Set(sentry.SentryTraceHeader, formatSentryTrace(spanContext.TraceID(), spanContext.SpanID(), sampled))

	if !dsc.HasEntries() {
		return
	}
	b := mergeDSCToBaggage(dsc, common.BaggageFromContext(ctx))
	if b.Len() > 0 {
		carrier.Set(sentry.SentryBaggageHeader, b.String())
	}
}

// Extract reads cross-cutting concerns from the carrier into a Context.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#extract
func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	sentryTraceHeader := carrier.Get(sentry.SentryTraceHeader)
	if sentryTraceHeader != "" {
		ctx = common.WithSentryTraceHeader(ctx, sentryTraceHeader)
		if traceParentContext, valid := sentry.ParseTraceParentContext([]byte(sentryTraceHeader)); valid {
			// Save traceParentContext because we'll at least need to know the original "sampled"
			// value in the span processor.
			ctx = common.WithTraceParentContext(ctx, traceParentContext)

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
			ctx = common.WithBaggage(ctx, parsedBaggage)
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

	return common.WithDynamicSamplingContext(ctx, dynamicSamplingContext)
}

// Fields returns a list of fields that will be used by the propagator.
//
// https://opentelemetry.io/docs/reference/specification/context/api-propagators/#fields
func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}

func mergeDSCToBaggage(dsc sentry.DynamicSamplingContext, base baggage.Baggage) baggage.Baggage {
	for k, v := range dsc.Entries {
		member, err := baggage.NewMember(sentryPrefix+k, v)
		if err != nil {
			continue
		}
		if updated, err := base.SetMember(member); err == nil {
			base = updated
		}
	}
	return base
}

func formatSentryTrace(trace trace.TraceID, span trace.SpanID, sampled bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s-%s", trace, span)
	if sampled {
		b.WriteString("-1")
	} else {
		b.WriteString("-0")
	}
	return b.String()
}
