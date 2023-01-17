package sentryotel

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func setupPropagatorTest() (propagation.TextMapPropagator, propagation.TextMapCarrier) {
	propagator := NewSentryPropagator()
	carrier := propagation.MapCarrier{}
	return propagator, carrier
}

/// Fields
func TestFieldsReturnsRightSet(t *testing.T) {
	propagator, _ := setupPropagatorTest()
	fields := propagator.Fields()
	assertEqual(t, fields, []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader})
}

/// Inject

/// Extract

// No sentry-trace header, no baggage header
func TestExtractDoesNotChangeContextWithEmptyHeaders(t *testing.T) {
	propagator, carrier := setupPropagatorTest()

	ctx := propagator.Extract(context.Background(), carrier)

	assertEqual(t,
		ctx.Value(utils.DynamicSamplingContextKey()),
		sentry.DynamicSamplingContext{Entries: map[string]string{}, Frozen: true},
	)
}

// No sentry-trace header, unrelated baggage header
func TestExtractSetsUndefinedDynamicSamplingContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(sentry.SentryBaggageHeader, "othervendor=bla")

	ctx := propagator.Extract(context.Background(), carrier)

	assertEqual(t,
		ctx.Value(utils.DynamicSamplingContextKey()),
		sentry.DynamicSamplingContext{Entries: map[string]string{}, Frozen: true},
	)
}

// With sentry-trace header, no baggage header
func TestExtractSetsSentrySpanContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(
		sentry.SentryTraceHeader,
		"d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
	)

	ctx := propagator.Extract(context.Background(), carrier)

	// Make sure that Extract added a proper span context to context
	spanContext := trace.SpanContextFromContext(ctx)
	spanId, _ := trace.SpanIDFromHex("6e0c63257de34c92")
	traceId, _ := trace.TraceIDFromHex("d4cda95b652f4a1592b449d5929fda1b")
	assertEqual(t,
		spanContext,
		trace.NewSpanContext(trace.SpanContextConfig{
			Remote:     true,
			SpanID:     spanId,
			TraceID:    traceId,
			TraceFlags: trace.FlagsSampled,
		}),
	)
}

// With sentry-trace header, no baggage header
func TestExtractHandlesInvalidTraceHeader(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(
		sentry.SentryTraceHeader,
		// Invalid trace value
		"xxx",
	)

	ctx := propagator.Extract(context.Background(), carrier)

	// Span context should be invalid
	spanContext := trace.SpanContextFromContext(ctx)
	assertEqual(t, spanContext.IsValid(), false)
}

// No sentry-trace header, with baggage header
func TestExtractSetsDefinedDynamicSamplingContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(
		sentry.SentryBaggageHeader,
		"othervendor=bla,sentry-environment=production,sentry-release=1.0.0,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b",
	)

	ctx := propagator.Extract(context.Background(), carrier)

	assertEqual(t,
		ctx.Value(utils.DynamicSamplingContextKey()),
		sentry.DynamicSamplingContext{
			Entries: map[string]string{
				"environment": "production",
				"public_key":  "abc",
				"release":     "1.0.0",
				"trace_id":    "d4cda95b652f4a1592b449d5929fda1b",
				"transaction": "dsc-transaction",
			},
			Frozen: true},
	)
}

/// Integration tests

// Valid baggage and sentry-trace headers
func TestExtractAndInjectValidSentryTraceAndBaggage(t *testing.T) {
	propagator, incomingCarrier := setupPropagatorTest()
	outgoingCarrier := propagation.MapCarrier{}
	incomingCarrier.Set(
		sentry.SentryBaggageHeader,
		"sentry-environment=production,sentry-release=1.0.0,othervendor=bla,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b",
	)
	incomingCarrier.Set(
		sentry.SentryTraceHeader,
		"d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
	)

	ctx := propagator.Extract(context.Background(), incomingCarrier)
	propagator.Inject(ctx, outgoingCarrier)

	assertMapCarrierEqual(t,
		outgoingCarrier,
		propagation.MapCarrier{
			"baggage":        "sentry-environment=production,sentry-release=1.0.0,othervendor=bla,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b",
			"sentry-tracing": "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
		},
	)
}

// No sentry-trace header, and baggage without sentry values
func TestExtractAndInjectNoSentryTraceAndExistingBaggage(t *testing.T) {
	propagator, incomingCarrier := setupPropagatorTest()
	outgoingCarrier := propagation.MapCarrier{}
	incomingCarrier.Set(
		sentry.SentryBaggageHeader,
		"othervendor=bla",
	)

	ctx := propagator.Extract(context.Background(), incomingCarrier)
	propagator.Inject(ctx, outgoingCarrier)

	assertMapCarrierEqual(t,
		outgoingCarrier,
		propagation.MapCarrier{
			"baggage": "othervendor=bla",
		},
	)
}
