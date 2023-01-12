package sentryotel

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/interal/utils"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"

	// TODO(anton): is it ok to use this module?
	otelTrace "go.opentelemetry.io/otel/trace"
)

type sentrySpanProcessor struct {
	// TODO(anton): any concurrency concerns here?
	SpanMap map[otelTrace.SpanID]*sentry.Span
}

var _ otelSdkTrace.SpanProcessor = (*sentrySpanProcessor)(nil)

func NewSentrySpanProcessor() otelSdkTrace.SpanProcessor {
	ssp := &sentrySpanProcessor{
		SpanMap: map[otelTrace.SpanID]*sentry.Span{},
	}

	return ssp
}

func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s otelSdkTrace.ReadWriteSpan) {
	fmt.Printf("\n--- SpanProcessor OnStart\nContext: %#v\nSpan: %#v\n", parent, s)

	otelSpanId := s.SpanContext().SpanID()
	otelParentSpanId := s.Parent().SpanID()

	var sentryParentSpan *sentry.Span
	if otelParentSpanId.IsValid() {
		sentryParentSpan = ssp.SpanMap[otelParentSpanId]
	}

	if sentryParentSpan != nil {
		span := sentryParentSpan.StartChild(s.Name())
		span.SpanID = sentry.SpanID(otelSpanId)
		span.StartTime = s.StartTime()

		ssp.SpanMap[otelSpanId] = span
	} else {
		// TODO(michi) add trace context
		transaction := sentry.StartTransaction(parent, s.Name())
		transaction.SpanID = sentry.SpanID(otelSpanId)
		transaction.StartTime = s.StartTime()

		ssp.SpanMap[otelSpanId] = transaction
	}
}

func (ssp *sentrySpanProcessor) OnEnd(s otelSdkTrace.ReadOnlySpan) {
	fmt.Printf("\n--- SpanProcessor OnEnd\nSpan: %#v\n", s)

	otelSpanId := s.SpanContext().SpanID()
	sentrySpan, ok := ssp.SpanMap[otelSpanId]
	if !ok || sentrySpan == nil {
		return
	}

	if utils.IsSentryRequestSpan(s) {
		delete(ssp.SpanMap, otelSpanId)
		return
	}

	// TODO(michi) export span.isTransaction
	if len(sentrySpan.TraceID) > 0 {
		updateTransactionWithOtelData(sentrySpan, s)
	} else {
		updateSpanWithOtelData(sentrySpan, s)
	}

	sentrySpan.EndTime = s.EndTime()
	sentrySpan.Finish()

	delete(ssp.SpanMap, otelSpanId)
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
	// transaction.setContext('otel', {
	// 	attributes: otelSpan.attributes,
	// 	resource: otelSpan.resource.attributes,
	//   });
	transaction.Status = utils.MapOtelStatus(s)

	attributes := utils.ParseSpanAttributes(s)
	transaction.Op = attributes.Op
	transaction.Source = attributes.Source

	// TODO(michi) the span name is only set on the scope
	// transaction.Name = attributes.description
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
