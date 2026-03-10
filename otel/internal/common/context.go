package common

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
)

// Context key types for propagating Sentry trace data through context.
type dynamicSamplingContextKey struct{}
type sentryTraceHeaderContextKey struct{}
type sentryTraceParentContextKey struct{}
type baggageContextKey struct{}

func DynamicSamplingContextFromContext(ctx context.Context) sentry.DynamicSamplingContext {
	if dsc, ok := ctx.Value(dynamicSamplingContextKey{}).(sentry.DynamicSamplingContext); ok {
		return dsc
	}
	return sentry.DynamicSamplingContext{}
}

func WithDynamicSamplingContext(ctx context.Context, dsc sentry.DynamicSamplingContext) context.Context {
	return context.WithValue(ctx, dynamicSamplingContextKey{}, dsc)
}

func TraceHeaderFromContext(ctx context.Context) string {
	if header, ok := ctx.Value(sentryTraceHeaderContextKey{}).(string); ok {
		return header
	}
	return ""
}

func WithSentryTraceHeader(ctx context.Context, header string) context.Context {
	return context.WithValue(ctx, sentryTraceHeaderContextKey{}, header)
}

func TraceParentContextFromContext(ctx context.Context) sentry.TraceParentContext {
	if tpc, ok := ctx.Value(sentryTraceParentContextKey{}).(sentry.TraceParentContext); ok {
		return tpc
	}
	return sentry.TraceParentContext{Sampled: sentry.SampledUndefined}
}

func WithTraceParentContext(ctx context.Context, tpc sentry.TraceParentContext) context.Context {
	return context.WithValue(ctx, sentryTraceParentContextKey{}, tpc)
}

func BaggageFromContext(ctx context.Context) baggage.Baggage {
	if b, ok := ctx.Value(baggageContextKey{}).(baggage.Baggage); ok {
		return b
	}
	return baggage.Baggage{}
}

func WithBaggage(ctx context.Context, b baggage.Baggage) context.Context {
	return context.WithValue(ctx, baggageContextKey{}, b)
}
