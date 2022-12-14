package sentryotel

import (
	"context"

	"go.opentelemetry.io/otel/sdk/trace"
)

type sentrySpanProcessor struct{}

var _ trace.SpanProcessor = (*sentrySpanProcessor)(nil)

func NewSentrySpanProcessor() trace.SpanProcessor {
	ssp := &sentrySpanProcessor{}

	return ssp
}

func (ssp *sentrySpanProcessor) OnStart(parent context.Context, s trace.ReadWriteSpan) {
}

func (ssp *sentrySpanProcessor) OnEnd(s trace.ReadOnlySpan) {
}

func (bsp *sentrySpanProcessor) Shutdown(ctx context.Context) error {
	var err error

	return err
}

func (bsp *sentrySpanProcessor) ForceFlush(ctx context.Context) error {
	var err error

	return err
}
