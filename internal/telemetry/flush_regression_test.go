package telemetry

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type blockedEnvelopeTransport struct {
	testutils.MockTelemetryTransport
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *blockedEnvelopeTransport) SendEnvelope(e *protocol.Envelope) error {
	t.once.Do(func() { close(t.started) })
	<-t.release
	return t.MockTelemetryTransport.SendEnvelope(e)
}

func TestFlushWaitsForInFlightSchedulerSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport := &blockedEnvelopeTransport{started: make(chan struct{}), release: make(chan struct{})}
		buffer := NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
		processor := NewProcessor(map[ratelimit.Category]Buffer[Item]{ratelimit.CategoryError: buffer}, transport, nil, nil, nil, nil)
		require.True(t, processor.Add(bwItem{id: "test"}))
		<-transport.started
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// The item is no longer in the buffer, but has not reached transport.
		assert.True(t, buffer.IsEmpty())
		assert.False(t, processor.FlushWithContext(ctx))
		assert.Empty(t, transport.GetSentEnvelopes())
		close(transport.release)
		require.True(t, processor.Flush(time.Second))
		require.Len(t, transport.GetSentEnvelopes(), 1)
		require.True(t, processor.Flush(time.Second))
		require.Len(t, transport.GetSentEnvelopes(), 1)
		processor.Close(time.Second)
	})
}
