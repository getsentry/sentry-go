package sentry_test

import (
	"sync"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockTransportEnvelopeListAndConcurrentReset(t *testing.T) {
	t.Parallel()
	transport := &sentry.MockTransport{}
	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{Trace: map[string]string{"key": "value"}}, protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"hello"}`)))
	require.NoError(t, transport.SendEnvelope(envelope))
	snapshot := transport.Envelopes()
	assert.Equal(t, "value", snapshot[0].Header.Trace["key"])
	snapshot[0] = nil
	assert.Equal(t, "hello", transport.Events()[0].Message)
	transport.Reset()
	assert.Empty(t, transport.Envelopes())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = transport.SendEnvelope(protocol.NewEnvelope(nil, protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"concurrent"}`))))
				_ = transport.Events()
				transport.Reset()
			}
		}()
	}
	wg.Wait()
}
