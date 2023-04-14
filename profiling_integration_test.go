package sentry

import (
	"testing"
)

func TestTraceProfiling(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
		Integrations: func(integrations []Integration) []Integration {
			return append(integrations, &profilingIntegration{})
		},
	})
	span := StartSpan(ctx, "top")
	span.Finish()

	// TODO actual test code.
}
