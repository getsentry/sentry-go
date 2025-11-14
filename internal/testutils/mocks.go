package testutils

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type MockTelemetryTransport struct {
	sentEnvelopes    []*protocol.Envelope
	rateLimited      map[string]bool
	sendError        error
	mu               sync.Mutex
	sendCount        int64
	rateLimitedCalls int64
	capacity         int
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

func (m *MockTelemetryTransport) IsRateLimited(category ratelimit.Category) bool {
	atomic.AddInt64(&m.rateLimitedCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rateLimited == nil {
		return false
	}
	return m.rateLimited[string(category)]
}

func (m *MockTelemetryTransport) HasCapacity() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.capacity == 0 {
		return true
	}
	return int(m.sendCount) < m.capacity
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

func (m *MockTelemetryTransport) SetRateLimited(category string, limited bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rateLimited == nil {
		m.rateLimited = make(map[string]bool)
	}
	m.rateLimited[category] = limited
}

func (m *MockTelemetryTransport) GetSendCount() int64 {
	return atomic.LoadInt64(&m.sendCount)
}

func (m *MockTelemetryTransport) GetRateLimitedCalls() int64 {
	return atomic.LoadInt64(&m.rateLimitedCalls)
}

func (m *MockTelemetryTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentEnvelopes = nil
	m.rateLimited = nil
	atomic.StoreInt64(&m.sendCount, 0)
	atomic.StoreInt64(&m.rateLimitedCalls, 0)
	m.capacity = 0
}
