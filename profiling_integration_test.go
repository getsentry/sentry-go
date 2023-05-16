package sentry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTraceProfiling(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		Transport:          transport,
		EnableTracing:      true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: 1.0,
	})
	span := StartSpan(ctx, "top")
	doWorkFor(100 * time.Millisecond)
	span.Finish()

	var require = require.New(t)
	require.Equal(1, len(transport.events))
	var event = transport.events[0]
	require.Equal(transactionType, event.Type)
	require.NotNil(event.transactionProfile)
	var profileInfo = event.transactionProfile
	require.Equal("go", profileInfo.Platform)
	require.Equal(span.TraceID.String(), profileInfo.Transaction.TraceID)
	validateProfile(t, profileInfo.Trace, span.EndTime.Sub(span.StartTime))
}
