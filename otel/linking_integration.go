package sentryotel

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/common"
)

type integration struct{}

// NewOtelIntegration registers OpenTelemetry linking with Sentry.
//
// It links captured Sentry errors, logs, and metrics to the active
// OpenTelemetry trace when a context carrying an active OTel span is used.
func NewOtelIntegration() sentry.Integration {
	return integration{}
}

func (integration) Name() string {
	return "OTel"
}

func (integration) SetupOnce(*sentry.Client) {}

func (integration) ResolveTraceContext(ctx context.Context) (sentry.TraceID, sentry.SpanID, sentry.Sampled, bool) {
	return common.ResolveTraceContextWithSampling(ctx)
}
