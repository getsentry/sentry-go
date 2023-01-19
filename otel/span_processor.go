package sentryotel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
	"go.opentelemetry.io/otel/attribute"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
	otelTrace "go.opentelemetry.io/otel/trace"
)

type SentrySpanMap struct {
	spanMap map[otelTrace.SpanID]*sentry.Span
	mu      sync.RWMutex
}

func (ssm *SentrySpanMap) Get(otelSpandID otelTrace.SpanID) (*sentry.Span, bool) {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()
	span, ok := ssm.spanMap[otelSpandID]
	return span, ok
}

func (ssm *SentrySpanMap) Set(otelSpandID otelTrace.SpanID, sentrySpan *sentry.Span) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap[otelSpandID] = sentrySpan
}

func (ssm *SentrySpanMap) Delete(otelSpandID otelTrace.SpanID) {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	delete(ssm.spanMap, otelSpandID)
}

func (ssm *SentrySpanMap) Clear() {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()
	ssm.spanMap = make(map[otelTrace.SpanID]*sentry.Span)
}

var sentrySpanMap = SentrySpanMap{spanMap: make(map[otelTrace.SpanID]*sentry.Span)}

type sentrySpanProcessor struct{}

var _ otelSdkTrace.SpanProcessor = (*sentrySpanProcessor)(nil)

func NewSentrySpanProcessor() otelSdkTrace.SpanProcessor {
	ssp := &sentrySpanProcessor{}

	return ssp
}

func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s otelSdkTrace.ReadWriteSpan) {
	fmt.Printf("\n--- SpanProcessor OnStart\nContext: %#v\nSpan: %#v\n", parent, s)

	otelSpanId := s.SpanContext().SpanID()
	otelParentSpanId := s.Parent().SpanID()

	var sentryParentSpan *sentry.Span
	if otelParentSpanId.IsValid() {
		sentryParentSpan, _ = sentrySpanMap.Get(otelParentSpanId)
	}

	if sentryParentSpan != nil {
		span := sentryParentSpan.StartChild(s.Name())
		span.SpanID = sentry.SpanID(otelSpanId)
		span.StartTime = s.StartTime()

		sentrySpanMap.Set(otelSpanId, span)
	} else {
		traceParentContext, _ := parent.Value(utils.SentryTraceParentContextKey()).(sentry.TraceParentContext)

		transaction := sentry.StartTransaction(
			parent,
			s.Name(),
			sentry.SpanSampled(traceParentContext.Sampled),
		)
		transaction.SpanID = sentry.SpanID(otelSpanId)
		transaction.TraceID = traceParentContext.TraceID
		transaction.ParentSpanID = traceParentContext.ParentSpanID
		transaction.StartTime = s.StartTime()
		if dynamicSamplingContext, valid := parent.Value(utils.DynamicSamplingContextKey()).(sentry.DynamicSamplingContext); valid {
			transaction.SetDynamicSamplingContext(dynamicSamplingContext)
		}

		sentrySpanMap.Set(otelSpanId, transaction)
	}
}

func (ssp *sentrySpanProcessor) OnEnd(s otelSdkTrace.ReadOnlySpan) {
	fmt.Printf("\n--- SpanProcessor OnEnd\nSpan: %#v\n", s)

	otelSpanId := s.SpanContext().SpanID()
	sentrySpan, ok := sentrySpanMap.Get(otelSpanId)
	if !ok || sentrySpan == nil {
		return
	}

	if utils.IsSentryRequestSpan(s) {
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

func (bsp *sentrySpanProcessor) Shutdown(ctx context.Context) error {
	var err error

	return err
}

func (bsp *sentrySpanProcessor) ForceFlush(ctx context.Context) error {
	var err error

	// TODO(michi) should we make this configurable?
	defer sentry.Flush(2 * time.Second)

	return err
}

func updateTransactionWithOtelData(transaction *sentry.Span, s otelSdkTrace.ReadOnlySpan) {
	// TODO(michi) This is crazy inefficient
	attributes := map[attribute.Key]string{}
	resource := map[attribute.Key]string{}

	for _, kv := range s.Attributes() {
		attributes[kv.Key] = kv.Value.AsString()
	}
	for _, kv := range s.Resource().Attributes() {
		resource[kv.Key] = kv.Value.AsString()
	}

	// TODO(michi) We might need to set this somewhere else then on the scope
	sentry.CurrentHub().Scope().SetContext("otel", map[string]interface{}{
		"attributes": attributes,
		"resource":   resource,
	})

	spanAttributes := utils.ParseSpanAttributes(s)

	transaction.Status = utils.MapOtelStatus(s)
	transaction.Op = spanAttributes.Op
	transaction.Source = spanAttributes.Source
	// TODO(michi) We might need to set this somewhere else then on the scope
	sentry.CurrentHub().Scope().SetTransaction(spanAttributes.Description)
}

func updateSpanWithOtelData(span *sentry.Span, s otelSdkTrace.ReadOnlySpan) {
	spanAttributes := utils.ParseSpanAttributes(s)

	span.Status = utils.MapOtelStatus(s)
	span.Op = spanAttributes.Op
	span.Description = spanAttributes.Description
	span.SetData("otel.kind", s.SpanKind().String())
	for _, kv := range s.Attributes() {
		span.SetData(string(kv.Key), kv.Value.AsString())
	}
}
