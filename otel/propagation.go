package sentryotel

import (
	"context"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/propagation"
)

type sentryPropagator struct{}

func (p sentryPropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) {

}

func (p sentryPropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	carrier.Get(sentry.SentryTraceHeader)
	return ctx
}

func (p sentryPropagator) Fields() []string {
	return []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader}
}

func NewSentryPropagator() propagation.TextMapPropagator {
	return sentryPropagator{}
}
