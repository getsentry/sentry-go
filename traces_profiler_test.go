package sentry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func testTraceProfiling(t *testing.T, rate float64) (*Span, *Event) {
	ticker := setupProfilerTestTicker()
	defer restoreProfilerTicker()

	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		Transport:          transport,
		EnableTracing:      true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: rate,
		Environment:        "env",
		Release:            "rel",
		Dist:               "dist",
	})
	span := StartSpan(ctx, "top")
	time.Sleep(time.Millisecond)
	ticker.Tick()
	span.Finish()

	require.Equal(t, 1, len(transport.events))
	return span, transport.events[0]
}

func TestTraceProfiling(t *testing.T) {
	var require = require.New(t)
	var timeBeforeStarting = time.Now()
	span, event := testTraceProfiling(t, 1.0)
	require.Equal(transactionType, event.Type)
	require.NotNil(event.transactionProfile)
	var profileInfo = event.transactionProfile
	require.Equal("go", profileInfo.Platform)
	require.Equal(event.Environment, profileInfo.Environment)
	require.Equal(event.Release, profileInfo.Release)
	require.Equal(event.Dist, profileInfo.Dist)
	require.GreaterOrEqual(profileInfo.Timestamp, timeBeforeStarting)
	require.LessOrEqual(profileInfo.Timestamp, time.Now())
	require.Equal(event.EventID, profileInfo.Transaction.ID)
	require.Greater(profileInfo.Transaction.ActiveThreadID, uint64(0))
	require.Equal(span.TraceID.String(), profileInfo.Transaction.TraceID)
	validateProfile(t, profileInfo.Trace, span.EndTime.Sub(span.StartTime))
}

func TestTraceProfilingDisabled(t *testing.T) {
	var require = require.New(t)
	_, event := testTraceProfiling(t, 0)
	require.Equal(transactionType, event.Type)
	require.Nil(event.transactionProfile)
}
