package sentryotel

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
)

type sentrySpanProcessor struct{}

var _ trace.SpanProcessor = (*sentrySpanProcessor)(nil)

func NewSentrySpanProcessor() trace.SpanProcessor {
	ssp := &sentrySpanProcessor{}

	return ssp
}

func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s trace.ReadWriteSpan) {
	hub := sentry.GetHubFromContext(parent)
	if hub == nil {
		return
	}

	scope := hub.Scope()
	if scope == nil {
		return
	}

	otelSpanId := s.SpanContext().SpanID().String()
	otelParentSpanId = s.Parent().SpanID().String()
}

func (ssp *sentrySpanProcessor) OnEnd(s trace.ReadOnlySpan) {
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
