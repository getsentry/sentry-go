package sentry

import (
	"testing"
	"time"
)

func TestTraceProfiling(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
		Integrations: func(integrations []Integration) []Integration {
			return append(integrations, &ProfilingIntegration{})
		},
	})
	span := StartSpan(ctx, "top")
	doWorkFor(350 * time.Millisecond)
	span.Finish()
	// TODO proper test
}
