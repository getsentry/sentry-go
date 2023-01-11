package sentryotel

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func setupPropagatorTest() (propagation.TextMapPropagator, propagation.TextMapCarrier) {
	propagator := NewSentryPropagator()
	carrier := propagation.MapCarrier{}
	return propagator, carrier
}

// Fields
func TestFieldsReturnsRightSet(t *testing.T) {
	propagator, _ := setupPropagatorTest()
	fields := propagator.Fields()
	assertEqual(t, fields, []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader})
}

// Inject

// Extract

func TestExtractDoesNotChangeContextWithEmptyHeaders(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "value")

	newCtx := propagator.Extract(ctx, carrier)

	assertEqual(t, ctx, newCtx)
}

func TestExtractSetsSentrySpanContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(
		sentry.SentryTraceHeader,
		"d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
	)

	ctx := propagator.Extract(context.Background(), carrier)

	spanContext := trace.SpanContextFromContext(ctx)
	spanId, _ := trace.SpanIDFromHex("6e0c63257de34c92")
	traceId, _ := trace.TraceIDFromHex("d4cda95b652f4a1592b449d5929fda1b")
	assertEqual(t, spanContext, trace.NewSpanContext(trace.SpanContextConfig{
		Remote:     true,
		SpanID:     spanId,
		TraceID:    traceId,
		TraceFlags: trace.FlagsSampled,
	}))
}

func TestExtractSetsDefinedDynamicSamplingContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(
		sentry.SentryBaggageHeader,
		"sentry-environment=production,sentry-release=1.0.0,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b",
	)

	propagator.Extract(context.Background(), carrier)

	t.Error("fixme")
}

func TestExtractSetsUndefinedDynamicSamplingContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(sentry.SentryBaggageHeader, "")
	ctx := context.Background()
	propagator.Extract(ctx, carrier)

	// assertEqual(ctx.Get())
	t.Error("fixme")
}
