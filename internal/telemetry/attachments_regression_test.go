package telemetry_test

import (
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProcessorFlush_EnvelopeCarriesScopeAttachments(t *testing.T) {
	t.Parallel()

	event := sentry.NewEvent()
	event.Message = "test with attachment"
	event.Level = sentry.LevelInfo

	scope := sentry.NewScope()
	scope.AddAttachment(&sentry.Attachment{
		Filename:    "test.txt",
		ContentType: "text/plain",
		Payload:     []byte("hello world"),
	})

	event = scope.ApplyToEvent(event, nil, nil)

	transport := &testutils.MockTelemetryTransport{}
	processor := telemetry.NewProcessor(
		map[ratelimit.Category]telemetry.Buffer[protocol.TelemetryItem]{
			ratelimit.CategoryError: telemetry.NewRingBuffer[protocol.TelemetryItem](
				ratelimit.CategoryError, 10, telemetry.OverflowPolicyDropOldest, 1, 0, nil,
			),
		},
		transport,
		&protocol.Dsn{},
		func() *protocol.SdkInfo {
			return &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}
		},
		nil,
	)
	t.Cleanup(func() { processor.Close(testutils.FlushTimeout()) })

	require.True(t, processor.Add(event), "add failed")
	require.True(t, processor.Flush(testutils.FlushTimeout()), "flush timed out")

	envelopes := transport.GetSentEnvelopes()
	require.Len(t, envelopes, 1, "expected a single envelope")
	require.Len(t, envelopes[0].Items, 2, "expected event and attachment envelope items")

	assert.Equal(t, protocol.EnvelopeItemTypeEvent, envelopes[0].Items[0].Header.Type)
	assert.Equal(t, protocol.EnvelopeItemTypeAttachment, envelopes[0].Items[1].Header.Type)
	assert.Equal(t, "test.txt", envelopes[0].Items[1].Header.Filename)
	assert.Equal(t, "text/plain", envelopes[0].Items[1].Header.ContentType)
	assert.Equal(t, []byte("hello world"), envelopes[0].Items[1].Payload)
}
