package testutils

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/protocol"
)

type MockTelemetryTransport struct {
	// SendFunc, when set, runs before an envelope is recorded; a non-nil error
	// rejects it. It runs without holding the mock's lock, so it may block.
	SendFunc func(context.Context, *protocol.Envelope) error

	sentEnvelopes []*protocol.Envelope
	mu            sync.Mutex
	sendCount     int64
}

func (m *MockTelemetryTransport) SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	atomic.AddInt64(&m.sendCount, 1)
	if m.SendFunc != nil {
		if err := m.SendFunc(ctx, envelope); err != nil {
			return err
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentEnvelopes = append(m.sentEnvelopes, envelope)
	return nil
}

func (m *MockTelemetryTransport) Flush(_ time.Duration) bool {
	return true
}

func (m *MockTelemetryTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (m *MockTelemetryTransport) Configure(_ interface{}) error {
	return nil
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
