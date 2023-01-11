package sentryotel

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"

	// TODO(anton): is it ok to use this module?
	trace "go.opentelemetry.io/otel/trace"
)

type sentrySpanProcessor struct {
	// TODO(anton): any concurrency concerns here?
	SpanMap map[trace.SpanID]*sentry.Span
}

var _ sdkTrace.SpanProcessor = (*sentrySpanProcessor)(nil)

func NewSentrySpanProcessor() sdkTrace.SpanProcessor {
	ssp := &sentrySpanProcessor{
		SpanMap: map[trace.SpanID]*sentry.Span{},
	}

	return ssp
}

func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s sdkTrace.ReadWriteSpan) {
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
		transaction := sentry.StartTransaction(parent, s.Name())
		transaction.SpanID = sentry.SpanID(otelSpanId)
		transaction.StartTime = s.StartTime()

		ssp.SpanMap[otelSpanId] = transaction
	}
}

func (ssp *sentrySpanProcessor) OnEnd(s sdkTrace.ReadOnlySpan) {
	fmt.Printf("\n--- SpanProcessor OnEnd\nSpan: %#v\n", s)

	otelSpanId := s.SpanContext().SpanID()
	sentrySpan, ok := ssp.SpanMap[otelSpanId]
	if !ok || sentrySpan == nil {
		return
	}

	// TODO(michi) export span.isTransaction
	if len(sentrySpan.TraceID) > 0 {
		// TODO(michi) add otel context
		sentrySpan.Status = sentry.SpanStatusOK
		sentrySpan.Op = s.Name()
		sentrySpan.Description = "Hello"
	}

	sentrySpan.EndTime = s.EndTime()
	sentrySpan.Finish()
}

func (bsp *sentrySpanProcessor) Shutdown(ctx context.Context) error {
	var err error

	return err
}

func (bsp *sentrySpanProcessor) ForceFlush(ctx context.Context) error {
	var err error

	defer sentry.Flush(2 * time.Second)

	return err
}
