package sentryotel

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	sdkTrace "go.opentelemetry.io/otel/sdk/trace"
	trace "go.opentelemetry.io/otel/trace"
)

type sentrySpanProcessor struct {
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
