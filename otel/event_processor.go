package sentryotel

import (
	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/common"
)

// NewEventProcessor creates a Sentry event processor that links captured
// errors to the active OpenTelemetry span by attaching the OTel trace and span IDs.
func NewEventProcessor() sentry.EventProcessor {
	return common.NewEventProcessor()
}
