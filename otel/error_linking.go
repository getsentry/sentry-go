package sentryotel

import (
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/common"
)

type errorLinkingIntegration struct{}

// NewErrorLinkingIntegration registers OpenTelemetry error linking with Sentry.
//
// It attaches the active OTel trace and span IDs to captured Sentry errors.
func NewErrorLinkingIntegration() sentry.Integration {
	return errorLinkingIntegration{}
}

func (errorLinkingIntegration) Name() string {
	return "OtelErrorLinking"
}

func (errorLinkingIntegration) SetupOnce(_ *sentry.Client) {
	sentry.AddGlobalEventProcessor(common.NewEventProcessor())
}
