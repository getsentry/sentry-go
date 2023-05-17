package sentry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testTraceProfiling(t *testing.T, rate float64) (*Span, *Event) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		Transport:          transport,
		EnableTracing:      true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: rate,
	})
	span := StartSpan(ctx, "top")
	doWorkFor(100 * time.Millisecond)
	span.Finish()

	require.Equal(t, 1, len(transport.events))
	return span, transport.events[0]
}

func TestTraceProfiling(t *testing.T) {
	var require = require.New(t)
	span, event := testTraceProfiling(t, 1.0)
	require.Equal(transactionType, event.Type)
	require.NotNil(event.transactionProfile)
	var profileInfo = event.transactionProfile
	require.Equal("go", profileInfo.Platform)
	require.Equal(span.TraceID.String(), profileInfo.Transaction.TraceID)
	validateProfile(t, profileInfo.Trace, span.EndTime.Sub(span.StartTime))
}

func TestTraceProfilingDisabled(t *testing.T) {
	var require = require.New(t)
	_, event := testTraceProfiling(t, 0)
	require.Equal(transactionType, event.Type)
	require.Nil(event.transactionProfile)
}
