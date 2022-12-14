package sentryotel

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
)

type sentryPropagator []propagation.TextMapPropagator

func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
}

func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {

	return ctx
}

func (p sentryPropagator) Fields() []string {
	return []string{}
}

func NewSentryPropagator(p ...propagation.TextMapPropagator) propagation.TextMapPropagator {
	return sentryPropagator(p)
}
