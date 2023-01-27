//go:build go1.18

package sentryotel

import (
	"context"
	"log"
	"testing"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/attribute"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func setupSpanProcessorTest() (otelSdkTrace.SpanProcessor, *otelSdkTrace.TracerProvider, trace.Tracer) {
	// Make sure that the global span map is empty
	sentrySpanMap.Clear()

	spanProcessor := NewSentrySpanProcessor()
	tp := otelSdkTrace.NewTracerProvider(otelSdkTrace.WithSampler(otelSdkTrace.AlwaysSample()))
	tp.RegisterSpanProcessor(spanProcessor)
	tracer := tp.Tracer("test-tracer")
	return spanProcessor, tp, tracer
}

func transactionName(span *sentry.Span) string {
	hub := sentry.GetHubFromContext(span.Context())
	if hub == nil {
		log.Fatal("Cannot extract transaction name: Hub is nil")
	}
	scope := hub.Scope()
	if scope == nil {
		log.Fatal("Cannot extract transaction name: Hub is nil")
	}
	return scope.Transaction()
}

func emptyContextWithSentry() context.Context {
	client, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://abc@example.com/123",
		Environment:      "testing",
		Release:          "1.2.3",
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        &TransportMock{},
	})
	hub := sentry.NewHub(client, sentry.NewScope())
	return sentry.SetHubOnContext(context.Background(), hub)
}

func getSentryTransportFromContext(ctx context.Context) *TransportMock {
	hub := sentry.GetHubFromContext(ctx)
	transport, ok := hub.Client().Transport.(*TransportMock)
	if !ok {
		log.Fatal(
			"Cannot get mock transport from ",
		)
	}
	return transport
}

func TestNewSentrySpanProcessor(t *testing.T) {
	spanProcessor := NewSentrySpanProcessor()

	sentrySpanProcessor, valid := spanProcessor.(*sentrySpanProcessor)
	if !valid {
		t.Errorf(
			"Invalid type returned by the span processor constructor: %#v\n",
			sentrySpanProcessor,
		)
	}
}

func TestSpanProcessorShutdown(t *testing.T) {
	spanProcessor, _, tracer := setupSpanProcessorTest()
	ctx := emptyContextWithSentry()
	tracer.Start(emptyContextWithSentry(), "spanName")

	assertEqual(t, sentrySpanMap.Len(), 1)

	spanProcessor.Shutdown(ctx)

	// The span map should be empty
	assertEqual(t, sentrySpanMap.Len(), 0)
}

func TestSpanProcessorForceFlush(t *testing.T) {
	// This test is pretty naive at the moment and just checks that
	// ForceFlush() doesn't crash or return an error. Ideally we test it
	// with a Sentry transport that can be checked to see if events were
	// actually flushed.
	spanProcessor, _, tracer := setupSpanProcessorTest()
	ctx, span := tracer.Start(emptyContextWithSentry(), "spanName")
	span.End()

	err := spanProcessor.ForceFlush(ctx)
	if err != nil {
		t.Errorf("Error from ForceFlush(): %v", err)
	}
}

func TestSentrySpanProcessorOnStartRootSpan(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	_, otelSpan := tracer.Start(emptyContextWithSentry(), "spanName")

	if sentrySpanMap.Len() != 1 {
		t.Errorf("Span map size is %d, expected: 1", sentrySpanMap.Len())
	}
	sentrySpan, ok := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())
	if !ok {
		t.Errorf("Sentry span not found in the map")
	}

	otelTraceId := otelSpan.SpanContext().TraceID()
	otelSpanId := otelSpan.SpanContext().SpanID()
	// TODO(anton): use a simple "assert", not "assertEqual"
	assertEqual(t, otelSpan.SpanContext().IsValid(), true)
	assertEqual(t, sentrySpan.SpanID.String(), otelSpanId.String())
	assertEqual(t, sentrySpan.TraceID.String(), otelTraceId.String())
	assertEqual(t, sentrySpan.ParentSpanID, sentry.SpanID{})
	assertEqual(t, sentrySpan.IsTransaction(), true)
	assertEqual(t, sentrySpan.ToBaggage(), "")
	assertEqual(t, sentrySpan.Sampled, sentry.SampledTrue)
	assertEqual(t, transactionName(sentrySpan), "spanName")
}

func TestSentrySpanProcessorOnStartWithTraceParentContext(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()

	// Sentry context
	ctx := context.WithValue(
		emptyContextWithSentry(),
		sentryTraceParentContextKey{},
		sentry.TraceParentContext{
			TraceID:      TraceIDFromHex("d4cda95b652f4a1592b449d5929fda1b"),
			ParentSpanID: SpanIDFromHex("6e0c63257de34c92"),
			Sampled:      sentry.SampledFalse,
		},
	)
	dsc := sentry.DynamicSamplingContext{
		Frozen:  true,
		Entries: map[string]string{"environment": "dev"},
	}
	ctx = context.WithValue(ctx, dynamicSamplingContextKey{}, dsc)
	// Otel span context
	ctx = trace.ContextWithSpanContext(
		ctx,
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    otelTraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
			SpanID:     otelSpanIDFromHex("b72fa28504b07285"),
			TraceFlags: trace.FlagsSampled,
		}),
	)
	_, otelSpan := tracer.Start(ctx, "spanName")

	if sentrySpanMap.Len() != 1 {
		t.Errorf("Span map size is %d, expected: 1", sentrySpanMap.Len())
	}
	sentrySpan, ok := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())
	if !ok {
		t.Errorf("Sentry span not found in the map")
	}

	assertEqual(t, otelSpan.SpanContext().IsValid(), true)
	assertEqual(t, sentrySpan.SpanID.String(), otelSpan.SpanContext().SpanID().String())
	// We're currently taking trace id and parent span id from the otel span context,
	// (not sentry-trace header), mostly to be aligned with other SDKs.
	assertEqual(t, sentrySpan.TraceID.String(), "bc6d53f15eb88f4320054569b8c553d4")
	assertEqual(t, sentrySpan.ParentSpanID, SpanIDFromHex("b72fa28504b07285"))
	assertEqual(t, sentrySpan.IsTransaction(), true)
	assertEqual(t, sentrySpan.ToBaggage(), "sentry-environment=dev")
	assertEqual(t, sentrySpan.Sampled, sentry.SampledFalse)
	assertEqual(t, transactionName(sentrySpan), "spanName")
}

func TestSentrySpanProcessorOnStartWithExistingParentSpan(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()

	// Otel span context
	ctx := trace.ContextWithSpanContext(
		emptyContextWithSentry(),
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    otelTraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
			SpanID:     otelSpanIDFromHex("b72fa28504b07285"),
			TraceFlags: trace.FlagsSampled,
		}),
	)
	ctx, otelRootSpan := tracer.Start(ctx, "rootSpan")
	_, otelChildSpan := tracer.Start(ctx, "childSpan")

	if sentrySpanMap.Len() != 2 {
		t.Errorf("Span map size is %d, expected: 2", sentrySpanMap.Len())
	}

	sentryTransaction, ok1 := sentrySpanMap.Get(otelRootSpan.SpanContext().SpanID())
	if !ok1 {
		t.Errorf("Sentry span not found in the map")
	}
	sentryChildSpan, ok2 := sentrySpanMap.Get(otelChildSpan.SpanContext().SpanID())
	if !ok2 {
		t.Errorf("Sentry span not found in the map")
	}

	assertEqual(t, otelChildSpan.SpanContext().IsValid(), true)
	assertEqual(t, otelRootSpan.SpanContext().IsValid(), true)
	assertEqual(t, sentryChildSpan.ParentSpanID, sentryTransaction.SpanID)
	assertEqual(t, sentryChildSpan.SpanID.String(), otelChildSpan.SpanContext().SpanID().String())
	assertEqual(t, sentryChildSpan.TraceID.String(), "bc6d53f15eb88f4320054569b8c553d4")
	assertEqual(t, sentryChildSpan.IsTransaction(), false)
	assertEqual(t, transactionName(sentryChildSpan), "rootSpan")
	assertEqual(t, sentryChildSpan.Op, "childSpan")
}

func TestSentrySpanProcessorOnEndForTransaction(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	ctx, otelSpan := tracer.Start(
		emptyContextWithSentry(),
		"transactionName",
		trace.WithAttributes(
			attribute.String("key1", "value1"),
			attribute.String("key2", "value2"),
		),
	)
	sentryTransaction, _ := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())
	assertEqual(t, sentryTransaction.EndTime.IsZero(), true)

	otelSpan.End()
	// The span map should be empty
	assertEqual(t, sentrySpanMap.Len(), 0)
	// EndTime should be populated
	assertEqual(t, sentryTransaction.EndTime.IsZero(), false)

	sentryTransport := getSentryTransportFromContext(ctx)
	events := sentryTransport.Events()
	assertEqual(t, len(events), 1)

	otelContextGot := events[0].Contexts["otel"]
	assertEqual(
		t,
		otelContextGot,
		map[string]interface{}{
			"attributes": map[attribute.Key]string{
				"key1": "value1",
				"key2": "value2",
			},
			"resource": map[attribute.Key]string{
				"service.name":           "unknown_service:otel.test",
				"telemetry.sdk.language": "go",
				"telemetry.sdk.name":     "opentelemetry",
				"telemetry.sdk.version":  "1.11.2",
			},
		},
	)
}

func TestSentrySpanProcessorOnEndWithChildSpan(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	ctx, otelRootSpan := tracer.Start(emptyContextWithSentry(), "rootSpan")
	_, otelChildSpan := tracer.Start(ctx, "childSpan")
	sentryTransaction, _ := sentrySpanMap.Get(otelRootSpan.SpanContext().SpanID())
	sentryChildSpan, _ := sentrySpanMap.Get(otelChildSpan.SpanContext().SpanID())
	otelChildSpan.End()
	otelRootSpan.End()

	// The span map should be empty
	assertEqual(t, sentrySpanMap.Len(), 0)
	// EndTime should be populated
	assertEqual(t, sentryTransaction.EndTime.IsZero(), false)
	assertEqual(t, sentryChildSpan.EndTime.IsZero(), false)
}

func TestOnEndDoesNotFinishSentryRequests(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	_, otelSpan := tracer.Start(
		emptyContextWithSentry(),
		"POST to Sentry",
		// Hostname is same as in Sentry DSN
		trace.WithAttributes(attribute.String("http.url", "https://example.com/sub/route")),
	)
	sentrySpan, _ := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())

	otelSpan.End()
	// The span map should be empty
	assertEqual(t, sentrySpanMap.Len(), 0)
	// EndTime should NOT be populated
	assertEqual(t, sentrySpan.EndTime.IsZero(), true)
}
