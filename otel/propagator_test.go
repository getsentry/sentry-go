//go:build go1.18

package sentryotel

import (
	"context"
	"fmt"
	"testing"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func setupPropagatorTest() (propagation.TextMapPropagator, propagation.MapCarrier) {
	// Make sure that the global span map is empty
	sentrySpanMap.Clear()

	propagator := NewSentryPropagator()
	carrier := propagation.MapCarrier{}
	return propagator, carrier
}

type transactionTestContext struct {
	name    string
	traceID string
	spanID  string
	sampled sentry.Sampled
}

type otelTestContext struct {
	traceId    string
	spanId     string
	traceFlags trace.TraceFlags
}

func createTransactionAndMaybeSpan(transactionContext transactionTestContext, withSpan bool) {
	client, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:           "https://abc@example.com/123",
		Environment:   "testing",
		Release:       "1.2.3",
		EnableTracing: true,
	})
	hub := sentry.NewHub(client, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), hub)

	transaction := sentry.StartTransaction(
		ctx,
		transactionContext.name,
		sentry.SpanSampled(transactionContext.sampled),
	)

	fmt.Printf("Transaction: %#v\n", transaction)

	transaction.TraceID = TraceIDFromHex(transactionContext.traceID)
	transaction.SpanID = SpanIDFromHex(transactionContext.spanID)
	transaction.SetDynamicSamplingContext(sentry.DynamicSamplingContextFromTransaction(transaction))

	sentrySpanMap.Set(trace.SpanID(transaction.SpanID), transaction)
	if withSpan {
		span := transaction.StartChild("op")
		sentrySpanMap.Set(trace.SpanID(span.SpanID), span)
	}
}

/// Fields
func TestFieldsReturnsRightSet(t *testing.T) {
	propagator, _ := setupPropagatorTest()
	fields := propagator.Fields()
	assertEqual(t, fields, []string{"sentry-trace", "baggage"})
}

/// Inject

func TestInjectDoesNothingOnEmptyContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()

	propagator.Inject(context.Background(), carrier)

	assertMapCarrierEqual(t,
		carrier,
		propagation.MapCarrier{},
	)
}

func TestInjectUsesSentryTraceOnEmptySpan(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	ctx := context.WithValue(
		context.Background(),
		sentryTraceHeaderContextKey{},
		"d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
	)

	propagator.Inject(ctx, carrier)

	assertMapCarrierEqual(t,
		carrier,
		propagation.MapCarrier{"sentry-trace": "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"},
	)
}

func TestInjectUsesBaggageOnEmptySpan(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	bag, _ := baggage.Parse("key1=value1;value2, key2=value2")
	ctx := baggage.ContextWithBaggage(context.Background(), bag)

	propagator.Inject(ctx, carrier)

	assertMapCarrierEqual(t,
		carrier,
		propagation.MapCarrier{"baggage": "key1=value1;value2,key2=value2"},
	)
}

func TestInjectUsesSetsValidTraceFromTransaction(t *testing.T) {
	tests := []struct {
		name                     string
		otelSpanContext          otelTestContext
		sentryTransactionContext transactionTestContext
		baggage                  *string
		sentryTrace              *string
	}{
		{
			name: "should set baggage and sentry-trace when sampled",
			otelSpanContext: otelTestContext{
				traceId:    "d4cda95b652f4a1592b449d5929fda1b",
				spanId:     "6e0c63257de34c92",
				traceFlags: trace.FlagsSampled,
			},
			sentryTransactionContext: transactionTestContext{
				name:    "sampled-transaction",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledTrue,
			},
			baggage:     stringPtr("sentry-environment=testing,sentry-release=1.2.3,sentry-transaction=sampled-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b,sentry-sample_rate=1"),
			sentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"),
		},
		{
			name: "should set proper baggage and sentry-trace when not sampled",
			otelSpanContext: otelTestContext{
				traceId:    "d4cda95b652f4a1592b449d5929fda1b",
				spanId:     "6e0c63257de34c92",
				traceFlags: trace.FlagsSampled,
			},
			sentryTransactionContext: transactionTestContext{
				name:    "not-sampled-transaction",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledFalse,
			},
			baggage:     stringPtr("sentry-environment=testing,sentry-release=1.2.3,sentry-transaction=not-sampled-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b"),
			sentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-0"),
		},
		{
			name: "should NOT set headers when traceId is empty",
			otelSpanContext: otelTestContext{
				traceId:    "",
				spanId:     "6e0c63257de34c92",
				traceFlags: trace.FlagsSampled,
			},
			sentryTransactionContext: transactionTestContext{
				name:    "transaction-name",
				traceID: "",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledTrue,
			},
			baggage:     nil,
			sentryTrace: nil,
		},
		{
			name: "should NOT set headers when spanId is empty",
			otelSpanContext: otelTestContext{
				traceId:    "d4cda95b652f4a1592b449d5929fda1b",
				spanId:     "",
				traceFlags: trace.FlagsSampled,
			},
			sentryTransactionContext: transactionTestContext{
				name:    "transaction-name",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "",
				sampled: sentry.SampledTrue,
			},
			baggage:     nil,
			sentryTrace: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			propagator, carrier := setupPropagatorTest()
			traceId, _ := trace.TraceIDFromHex(tt.otelSpanContext.traceId)
			spanId, _ := trace.SpanIDFromHex(tt.otelSpanContext.spanId)
			otelSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    traceId,
				SpanID:     spanId,
				TraceFlags: tt.otelSpanContext.traceFlags,
			})
			ctx := trace.ContextWithSpanContext(context.Background(), otelSpanContext)
			createTransactionAndMaybeSpan(tt.sentryTransactionContext, false)

			propagator.Inject(ctx, carrier)

			expectedCarrier := propagation.MapCarrier{}
			if tt.baggage != nil {
				expectedCarrier["baggage"] = *tt.baggage
			}
			if tt.sentryTrace != nil {
				expectedCarrier["sentry-trace"] = *tt.sentryTrace
			}
			assertMapCarrierEqual(t, carrier, expectedCarrier)
		})
	}

}

/// Extract

// No sentry-trace header, no baggage header
func TestExtractDoesNotChangeContextWithEmptyHeaders(t *testing.T) {
	propagator, carrier := setupPropagatorTest()

	ctx := propagator.Extract(context.Background(), carrier)

	assertEqual(t,
		ctx.Value(dynamicSamplingContextKey{}),
		sentry.DynamicSamplingContext{Entries: map[string]string{}, Frozen: false},
	)
}

// No sentry-trace header, 3rd-party baggage header
func TestExtractSetsUndefinedDynamicSamplingContext(t *testing.T) {
	propagator, carrier := setupPropagatorTest()
	carrier.Set(sentry.SentryBaggageHeader, "othervendor=bla")

	ctx := propagator.Extract(context.Background(), carrier)

	assertEqual(t,
		ctx.Value(dynamicSamplingContextKey{}),
		sentry.DynamicSamplingContext{Entries: map[string]string{}, Frozen: false},
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
		ctx.Value(dynamicSamplingContextKey{}),
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
			"baggage":      "sentry-environment=production,sentry-release=1.0.0,othervendor=bla,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b",
			"sentry-trace": "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
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
