package testutils

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/protocol"
)

type MockTelemetryTransport struct {
	sentEnvelopes []*protocol.Envelope
	sendError     error
	mu            sync.Mutex
	sendCount     int64
}

func (m *MockTelemetryTransport) SendEnvelope(envelope *protocol.Envelope) error {
	atomic.AddInt64(&m.sendCount, 1)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendError != nil {
		return m.sendError
	}

	m.sentEnvelopes = append(m.sentEnvelopes, envelope)
	return nil
}

func (m *MockTelemetryTransport) Flush(_ time.Duration) bool {
	return true
}

func (m *MockTelemetryTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (m *MockTelemetryTransport) Close() {
}

func (m *MockTelemetryTransport) GetSentEnvelopes() []*protocol.Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*protocol.Envelope, len(m.sentEnvelopes))
	copy(result, m.sentEnvelopes)
	return result
}

func (m *MockTelemetryTransport) GetSendCount() int64 {
	return atomic.LoadInt64(&m.sendCount)
}

func (m *MockTelemetryTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentEnvelopes = nil
	atomic.StoreInt64(&m.sendCount, 0)
}
