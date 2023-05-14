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

	for {
		_ = findPrimeNumber(1000)
		if time.Since(span.StartTime).Milliseconds() > 350 {
			break
		}
	}

	span.Finish()
	// TODO proper test
}

func findPrimeNumber(n int) int {
	count := 0
	a := 2
	for count < n {
		b := 2
		prime := true // to check if found a prime
		for b*b <= a {
			if a%b == 0 {
				prime = false
				break
			}
			b++
		}
		if prime {
			count++
		}
		a++
	}
	return a - 1
}
