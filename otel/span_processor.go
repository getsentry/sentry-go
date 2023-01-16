package sentryotel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"

	// TODO(anton): is it ok to use this module?
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

var sentrySpanMap SentrySpanMap

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
		// StartTransaction/StartSpan adds the trace context internally
		transaction := sentry.StartTransaction(parent, s.Name())
		transaction.SpanID = sentry.SpanID(otelSpanId)
		transaction.StartTime = s.StartTime()

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
	// TODO(michi) We might need to set this somewhere else then on the scope
	sentry.CurrentHub().Scope().SetContext("otel", map[string]interface{}{
		"attributes": s.Attributes(),
		"resource":   s.Resource().Attributes(),
	})

	transaction.Status = utils.MapOtelStatus(s)

	attributes := utils.ParseSpanAttributes(s)
	transaction.Op = attributes.Op
	transaction.Source = attributes.Source
	// TODO(michi) We might need to set this somewhere else then on the scope
	sentry.CurrentHub().Scope().SetTransaction(attributes.Description)
}

func updateSpanWithOtelData(span *sentry.Span, s otelSdkTrace.ReadOnlySpan) {
	// const { attributes, kind } = otelSpan;

	span.Status = utils.MapOtelStatus(s)
	// sentrySpan.setData('otel.kind', kind.valueOf());

	// Object.keys(attributes).forEach(prop => {
	//   const value = attributes[prop];
	//   sentrySpan.setData(prop, value);
	// });

	attributes := utils.ParseSpanAttributes(s)
	span.Op = attributes.Op
	span.Description = attributes.Description
}
