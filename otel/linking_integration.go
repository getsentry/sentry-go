package sentryotel

import (
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/common"
)

type integration struct{}

// NewLinkingIntegration registers OpenTelemetry linking with Sentry.
//
// It links captured Sentry errors, logs, and metrics to the active
// OpenTelemetry trace when a context carrying an active OTel span is used.
func NewLinkingIntegration() sentry.Integration {
	return integration{}
}

func (integration) Name() string {
	return "OTel"
}

func (integration) SetupOnce(client *sentry.Client) {
	client.SetExternalContextTraceResolver(common.ResolveTraceContext)
}
