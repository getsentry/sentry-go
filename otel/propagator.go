package sentryotel

import (
	"github.com/getsentry/sentry-go/otel/internal/common"
	"go.opentelemetry.io/otel/propagation"
)

func NewSentryPropagator() propagation.TextMapPropagator {
	return common.NewSentryPropagator(
		common.WithDSCSource(&sentrySpanMap),
	)
}
