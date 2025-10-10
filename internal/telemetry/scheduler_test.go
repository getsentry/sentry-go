package telemetry

import (
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
)

type testTelemetryItem struct {
	id       int
	data     string
	envelope *protocol.Envelope
}

func (t *testTelemetryItem) ToEnvelope(_ *protocol.Dsn) (*protocol.Envelope, error) {
	if t.envelope != nil {
		return t.envelope, nil
	}

	envelope := &protocol.Envelope{
		Header: &protocol.EnvelopeHeader{
			EventID: t.data,
		},
		Items: []*protocol.EnvelopeItem{
			{
				Header: &protocol.EnvelopeItemHeader{
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "` + t.data + `"}`),
			},
		},
	}
	return envelope, nil
}

func TestNewTelemetryScheduler(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffers := map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
		ratelimit.CategoryError: NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
		ratelimit.CategoryLog:   NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second),
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	if scheduler == nil {
		t.Fatal("Expected non-nil scheduler")
	}

	if len(scheduler.buffers) != 2 {
		t.Errorf("Expected 2 buffers, got %d", len(scheduler.buffers))
	}

	if scheduler.dsn != dsn {
		t.Error("Expected DSN to be set correctly")
	}

	if len(scheduler.currentCycle) == 0 {
		t.Error("Expected non-empty priority cycle")
	}

	criticalCount := 0
	mediumCount := 0
	for _, priority := range scheduler.currentCycle {
		switch priority {
		case ratelimit.PriorityCritical:
			criticalCount++
		case ratelimit.PriorityMedium:
			mediumCount++
		}
	}

	if criticalCount <= mediumCount {
		t.Errorf("Expected more critical priority slots (%d) than medium (%d)", criticalCount, mediumCount)
	}
}

func TestTelemetrySchedulerFlush(t *testing.T) {
	tests := []struct {
		name          string
		setupBuffers  func() map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]
		addItems      func(buffers map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible])
		expectedCount int64
	}{
		{
			name: "single category with multiple items",
			setupBuffers: func() map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible] {
				return map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
					ratelimit.CategoryError: NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
				}
			},
			addItems: func(buffers map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]) {
				for i := 1; i <= 5; i++ {
					buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: i, data: "test"})
				}
			},
			expectedCount: 5,
		},
		{
			name: "empty buffers",
			setupBuffers: func() map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible] {
				return map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
					ratelimit.CategoryError: NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryLog:   NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second),
				}
			},
			addItems:      func(_ map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]) {},
			expectedCount: 0,
		},
		{
			name: "multiple categories",
			setupBuffers: func() map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible] {
				return map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
					ratelimit.CategoryError:       NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryTransaction: NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryMonitor:     NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryMonitor, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryLog:         NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second),
				}
			},
			addItems: func(buffers map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]) {
				i := 0
				for category, buffer := range buffers {
					buffer.Offer(&testTelemetryItem{id: i + 1, data: string(category)})
					i++
				}
			},
			expectedCount: 4,
		},
		{
			name: "priority ordering - error and log",
			setupBuffers: func() map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible] {
				return map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
					ratelimit.CategoryError: NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryLog:   NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second),
				}
			},
			addItems: func(buffers map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]) {
				buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: 1, data: "error"})
				buffers[ratelimit.CategoryLog].Offer(&testTelemetryItem{id: 2, data: "log"})
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &testutils.MockTelemetryTransport{}
			dsn := &protocol.Dsn{}

			buffers := tt.setupBuffers()
			scheduler := NewScheduler(buffers, transport, dsn)

			tt.addItems(buffers)

			scheduler.Flush(time.Second)

			if transport.GetSendCount() != tt.expectedCount {
				t.Errorf("Expected %d items to be processed, got %d", tt.expectedCount, transport.GetSendCount())
			}

			for category, buffer := range buffers {
				if !buffer.IsEmpty() {
					t.Errorf("Expected buffer %s to be empty after flush", category)
				}
			}
		})
	}
}

func TestTelemetrySchedulerRateLimiting(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffer := NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
		ratelimit.CategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	transport.SetRateLimited("error", true)

	scheduler.Start()
	defer scheduler.Stop(100 * time.Millisecond)

	item := &testTelemetryItem{id: 1, data: "test"}
	buffer.Offer(item)
	scheduler.Signal()

	time.Sleep(200 * time.Millisecond)

	if transport.GetSendCount() > 0 {
		t.Errorf("Expected 0 items to be processed due to rate limiting, got %d", transport.GetSendCount())
	}

	if transport.GetRateLimitedCalls() == 0 {
		t.Error("Expected rate limit check to be called")
	}
}

func TestTelemetrySchedulerStartStop(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffer := NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
		ratelimit.CategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	scheduler.Start()
	scheduler.Start()

	item := &testTelemetryItem{id: 1, data: "test"}
	buffer.Offer(item)
	scheduler.Signal()

	scheduler.Stop(time.Second)
	scheduler.Stop(time.Second)

	if transport.GetSendCount() == 0 {
		t.Error("Expected at least 1 item to be processed")
	}
}

func TestTelemetrySchedulerContextCancellation(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffer := NewBuffer[protocol.EnvelopeConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]{
		ratelimit.CategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	scheduler.Start()

	for i := 1; i <= 5; i++ {
		item := &testTelemetryItem{id: i, data: "test"}
		buffer.Offer(item)
	}
	scheduler.Signal()

	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Stop(100 * time.Millisecond)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("Scheduler stop took too long")
	}
}
