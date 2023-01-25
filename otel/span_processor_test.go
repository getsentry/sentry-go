//go:build go1.18

package sentryotel

import (
	"context"
	"log"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/utils"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func setupSpanProcessorTest() (otelSdkTrace.SpanProcessor, *otelSdkTrace.TracerProvider) {
	// Make sure that the global span map is empty
	sentrySpanMap.Clear()

	spanProcessor := NewSentrySpanProcessor()
	tp := otelSdkTrace.NewTracerProvider(otelSdkTrace.WithSampler(otelSdkTrace.AlwaysSample()))
	return spanProcessor, tp
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
		Dsn:           "https://abc@example.com/123",
		Environment:   "testing",
		Release:       "1.2.3",
		EnableTracing: true,
		// FIXME(anton): Would be nice to have TransportMock here (and
		// other similar places)
	})
	hub := sentry.NewHub(client, sentry.NewScope())
	return sentry.SetHubOnContext(context.Background(), hub)
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

func TestSentrySpanProcessorOnStartRootSpan(t *testing.T) {
	spanProcessor, tp := setupSpanProcessorTest()
	tp.RegisterSpanProcessor(spanProcessor)

	_, otelSpan := tp.Tracer("test-tracer").Start(emptyContextWithSentry(), "spanName")

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
	assertEqual(t, sentrySpan.Sampled, sentry.SampledFalse)
	assertEqual(t, transactionName(sentrySpan), "spanName")
}

func TestSentrySpanProcessorOnStartWithTraceParentContext(t *testing.T) {
	spanProcessor, tp := setupSpanProcessorTest()
	tp.RegisterSpanProcessor(spanProcessor)

	// Sentry context
	ctx := context.WithValue(
		emptyContextWithSentry(),
		utils.SentryTraceParentContextKey(),
		sentry.TraceParentContext{
			TraceID:      TraceIDFromHex("d4cda95b652f4a1592b449d5929fda1b"),
			ParentSpanID: SpanIDFromHex("6e0c63257de34c92"),
			Sampled:      sentry.SampledTrue,
		},
	)
	dsc := sentry.DynamicSamplingContext{
		Frozen:  true,
		Entries: map[string]string{"environment": "dev"},
	}
	ctx = context.WithValue(
		ctx,
		utils.DynamicSamplingContextKey(),
		dsc,
	)
	// Otel span context
	ctx = trace.ContextWithSpanContext(
		ctx,
		trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    otelTraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
			SpanID:     otelSpanIDFromHex("b72fa28504b07285"),
			TraceFlags: trace.FlagsSampled,
		}),
	)
	_, otelSpan := tp.Tracer("test-tracer").Start(ctx, "spanName")

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
	assertEqual(t, sentrySpan.Sampled, sentry.SampledTrue)
	assertEqual(t, transactionName(sentrySpan), "spanName")
}

func TestSentrySpanProcessorOnStartWithExistingParentSpan(t *testing.T) {
	spanProcessor, tp := setupSpanProcessorTest()
	tp.RegisterSpanProcessor(spanProcessor)
	tracer := tp.Tracer("test-tracer")

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

func TestSentrySpanProcessorOnEndBasicFlow(t *testing.T) {
	spanProcessor, tp := setupSpanProcessorTest()
	tp.RegisterSpanProcessor(spanProcessor)

	_, otelSpan := tp.Tracer("test-tracer").Start(emptyContextWithSentry(), "spanName")
	sentrySpan, _ := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())

	assertEqual(t, sentrySpan.EndTime.IsZero(), true)

	otelSpan.End()

	// The span map should be empty
	assertEqual(t, sentrySpanMap.Len(), 0)
	// EndTime should be populated
	assertEqual(t, sentrySpan.EndTime.IsZero(), false)
}
