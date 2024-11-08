package sentryotel

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
)

type sentrySpanProcessor struct{}

// Singleton instance of the Sentry span processor.
// At the moment we do not support multiple instances.
var sentrySpanProcessorInstance *sentrySpanProcessor

func NewSentrySpanProcessor() otelSdkTrace.SpanProcessor {
	if sentrySpanProcessorInstance != nil {
		return sentrySpanProcessorInstance
	}
	sentry.AddGlobalEventProcessor(linkTraceContextToErrorEvent)
	sentrySpanProcessorInstance := &sentrySpanProcessor{}
	return sentrySpanProcessorInstance
}

// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/sdk.md#onstart
func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s otelSdkTrace.ReadWriteSpan) {
	otelSpanContext := s.SpanContext()
	otelSpanID := otelSpanContext.SpanID()
	otelTraceID := otelSpanContext.TraceID()
	otelParentSpanID := s.Parent().SpanID()

	var sentryParentSpan *sentry.Span
	if otelSpanContext.IsValid() {
		sentryParentSpan, _ = sentrySpanMap.Get(otelParentSpanID)
	}

	if sentryParentSpan != nil {
		span := sentryParentSpan.StartChild(s.Name())
		span.SpanID = sentry.SpanID(otelSpanID)
		span.StartTime = s.StartTime()

		sentrySpanMap.Set(otelSpanID, span)
	} else {
		traceParentContext := getTraceParentContext(parent)
		transaction := sentry.StartTransaction(
			parent,
			s.Name(),
			sentry.WithSpanSampled(traceParentContext.Sampled),
		)
		transaction.SpanID = sentry.SpanID(otelSpanID)
		transaction.TraceID = sentry.TraceID(otelTraceID)
		transaction.ParentSpanID = sentry.SpanID(otelParentSpanID)
		transaction.StartTime = s.StartTime()
		if dynamicSamplingContext, valid := parent.Value(dynamicSamplingContextKey{}).(sentry.DynamicSamplingContext); valid {
			transaction.SetDynamicSamplingContext(dynamicSamplingContext)
		}

		sentrySpanMap.Set(otelSpanID, transaction)
	}
}

// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/sdk.md#onendspan
func (ssp *sentrySpanProcessor) OnEnd(s otelSdkTrace.ReadOnlySpan) {
	otelSpanId := s.SpanContext().SpanID()
	sentrySpan, ok := sentrySpanMap.Get(otelSpanId)
	if !ok || sentrySpan == nil {
		return
	}

	if utils.IsSentryRequestSpan(sentrySpan.Context(), s) {
		sentrySpanMap.Delete(otelSpanId)
		return
	}

	if sentrySpan.IsTransaction() {
		updateTransactionWithOtelData(sentrySpan, s)
	} else {
		updateSpanWithOtelData(sentrySpan, s)
	}

	sentrySpan.EndTime = s.EndTime()
	sentrySpan.Finish()

	sentrySpanMap.Delete(otelSpanId)
}

// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/sdk.md#shutdown-1
func (ssp *sentrySpanProcessor) Shutdown(ctx context.Context) error {
	sentrySpanMap.Clear()
	// Note: according to the spec, "Shutdown MUST include the effects of ForceFlush".
	return ssp.ForceFlush(ctx)
}

// https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/trace/sdk.md#forceflush-1
func (ssp *sentrySpanProcessor) ForceFlush(ctx context.Context) error {
	return flushSpanProcessor(ctx)
}

func flushSpanProcessor(ctx context.Context) error {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	// TODO(michi) should we make this configurable?
	defer hub.Flush(2 * time.Second)
	return nil
}

func getTraceParentContext(ctx context.Context) sentry.TraceParentContext {
	traceParentContext, ok := ctx.Value(sentryTraceParentContextKey{}).(sentry.TraceParentContext)
	if !ok {
		traceParentContext.Sampled = sentry.SampledUndefined
	}
	return traceParentContext
}

func updateTransactionWithOtelData(transaction *sentry.Span, s otelSdkTrace.ReadOnlySpan) {
	// TODO(michi) This is crazy inefficient
	attributes := map[attribute.Key]interface{}{}
	resource := map[attribute.Key]interface{}{}

	for _, kv := range s.Attributes() {
		attributes[kv.Key] = kv.Value.AsInterface()
	}
	for _, kv := range s.Resource().Attributes() {
		resource[kv.Key] = kv.Value.AsInterface()
	}

	transaction.SetContext("otel", map[string]interface{}{
		"attributes": attributes,
		"resource":   resource,
	})

	spanAttributes := utils.ParseSpanAttributes(s)

	transaction.Status = utils.MapOtelStatus(s)
	transaction.Name = spanAttributes.Description
	transaction.Op = spanAttributes.Op
	transaction.Source = spanAttributes.Source
}

func updateSpanWithOtelData(span *sentry.Span, s otelSdkTrace.ReadOnlySpan) {
	spanAttributes := utils.ParseSpanAttributes(s)

	span.Status = utils.MapOtelStatus(s)
	span.Op = spanAttributes.Op
	span.Description = spanAttributes.Description
	span.SetData("otel.kind", s.SpanKind().String())
	for _, kv := range s.Attributes() {
		span.SetData(string(kv.Key), kv.Value.AsInterface())
	}
}
