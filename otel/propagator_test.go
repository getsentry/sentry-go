package sentryotel

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
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

func createTransactionAndMaybeSpan(transactionContext transactionTestContext, withSpan bool) trace.SpanContextConfig {
	transaction := sentry.StartTransaction(
		emptyContextWithSentry(),
		transactionContext.name,
		sentry.WithSpanSampled(transactionContext.sampled),
	)
	transaction.TraceID = TraceIDFromHex(transactionContext.traceID)
	transaction.SpanID = SpanIDFromHex(transactionContext.spanID)

	if withSpan {
		span := transaction.StartChild("op")
		// We want the child to have the SpanID from transactionContext, so
		// we "swap" span IDs from the transaction and the child span.
		transaction.SpanID = span.SpanID
		span.SpanID = SpanIDFromHex(transactionContext.spanID)
		sentrySpanMap.Set(trace.SpanID(span.SpanID), span)
	}
	sentrySpanMap.Set(trace.SpanID(transaction.SpanID), transaction)

	otelContext := trace.SpanContextConfig{
		TraceID:    otelTraceIDFromHex(transactionContext.traceID),
		SpanID:     otelSpanIDFromHex(transactionContext.spanID),
		TraceFlags: trace.FlagsSampled,
	}
	return otelContext
}

func TestNewSentryPropagator(t *testing.T) {
	propagator := NewSentryPropagator()

	if _, valid := propagator.(*sentryPropagator); !valid {
		t.Errorf(
			"Invalid type returned by the propagator constructor: %#v\n",
			propagator,
		)
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
	ctx := context.WithValue(context.Background(), baggageContextKey{}, bag)

	propagator.Inject(ctx, carrier)

	assertMapCarrierEqual(t,
		carrier,
		propagation.MapCarrier{"baggage": "key1=value1;value2,key2=value2"},
	)
}

func testInjectUsesSetsValidTrace(t *testing.T, withChildSpan bool) {
	tests := []struct {
		name                     string
		sentryTransactionContext transactionTestContext
		wantBaggage              *string
		wantSentryTrace          *string
	}{
		{
			name: "should set baggage and sentry-trace when sampled",
			sentryTransactionContext: transactionTestContext{
				name:    "sampled-transaction",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledTrue,
			},
			wantBaggage:     stringPtr("sentry-environment=testing,sentry-release=1.2.3,sentry-transaction=sampled-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b,sentry-sample_rate=1,sentry-sampled=true"),
			wantSentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"),
		},
		{
			name: "should set proper baggage and sentry-trace when not sampled",
			sentryTransactionContext: transactionTestContext{
				name:    "not-sampled-transaction",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledFalse,
			},
			wantBaggage:     stringPtr("sentry-environment=testing,sentry-release=1.2.3,sentry-transaction=not-sampled-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b,sentry-sampled=false"),
			wantSentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-0"),
		},
		{
			name: "should NOT set headers when traceId is empty",
			sentryTransactionContext: transactionTestContext{
				name:    "transaction-name",
				traceID: "",
				spanID:  "6e0c63257de34c92",
				sampled: sentry.SampledTrue,
			},
			wantBaggage:     nil,
			wantSentryTrace: nil,
		},
		{
			name: "should NOT set headers when spanId is empty",
			sentryTransactionContext: transactionTestContext{
				name:    "transaction-name",
				traceID: "d4cda95b652f4a1592b449d5929fda1b",
				spanID:  "",
				sampled: sentry.SampledTrue,
			},
			wantBaggage:     nil,
			wantSentryTrace: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			propagator, carrier := setupPropagatorTest()
			testContextConfig := createTransactionAndMaybeSpan(tt.sentryTransactionContext, withChildSpan)
			otelSpanContext := trace.NewSpanContext(testContextConfig)
			ctx := trace.ContextWithSpanContext(context.Background(), otelSpanContext)
			propagator.Inject(ctx, carrier)

			expectedCarrier := propagation.MapCarrier{}
			if tt.wantBaggage != nil {
				expectedCarrier["baggage"] = *tt.wantBaggage
			}
			if tt.wantSentryTrace != nil {
				expectedCarrier["sentry-trace"] = *tt.wantSentryTrace
			}
			assertMapCarrierEqual(t, carrier, expectedCarrier)
		})
	}
}

func TestInjectUsesSetsValidTraceFromTransaction(t *testing.T) {
	testInjectUsesSetsValidTrace(t, false)
}

func TestInjectUsesSetsValidTraceFromChildSpan(t *testing.T) {
	testInjectUsesSetsValidTrace(t, true)
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

func TestExtractAndInjectIntegration(t *testing.T) {
	tests := []struct {
		name          string
		inSentryTrace *string
		inBaggage     *string
	}{
		{
			name:          "valid sentry-trace and baggage",
			inSentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"),
			inBaggage:     stringPtr("sentry-environment=production,sentry-release=1.0.0,othervendor=bla,sentry-transaction=dsc-transaction,sentry-public_key=abc,sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b"),
		},
		{
			name:          "only sentry-trace, no baggage",
			inSentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"),
		},
		{
			name:          "valid sentry-trace and mixed baggage with special characters",
			inSentryTrace: stringPtr("d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1"),
			inBaggage:     stringPtr("sentry-transaction=GET%20POST,userId=Am%C3%A9lie, key1 = +++ , key2=%253B"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			propagator, incomingCarrier := setupPropagatorTest()

			if tt.inBaggage != nil {
				incomingCarrier.Set(
					"baggage",
					*tt.inBaggage,
				)
			}
			if tt.inSentryTrace != nil {
				incomingCarrier.Set(
					"sentry-trace",
					*tt.inSentryTrace,
				)
			}
			outgoingCarrier := propagation.MapCarrier{}

			ctx := propagator.Extract(context.Background(), incomingCarrier)
			propagator.Inject(ctx, outgoingCarrier)

			assertMapCarrierEqual(t,
				outgoingCarrier,
				incomingCarrier,
			)
		})
	}
}
