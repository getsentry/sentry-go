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
	category ratelimit.Category
}

func (t *testTelemetryItem) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	var payload string
	if t.GetCategory() == ratelimit.CategoryLog {
		payload = `{"type": "log", "timestamp": "2023-01-01T00:00:00Z", "logs": [{"level": "info", "body": "` + t.data + `"}]}`
	} else {
		payload = `{"message": "` + t.data + `"}`
	}

	return &protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type: protocol.EnvelopeItemTypeEvent,
		},
		Payload: []byte(payload),
	}, nil
}

func (t *testTelemetryItem) GetCategory() ratelimit.Category {
	if t.category != "" {
		return t.category
	}
	return ratelimit.CategoryError
}

func (t *testTelemetryItem) GetEventID() string {
	return t.data
}

func (t *testTelemetryItem) GetSdkInfo() *protocol.SdkInfo {
	return &protocol.SdkInfo{
		Name:    "test",
		Version: "1.0.0",
	}
}

func (t *testTelemetryItem) GetDynamicSamplingContext() map[string]string {
	return nil
}

func TestNewTelemetryScheduler(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffers := map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
	}

	sdkInfo := &protocol.SdkInfo{
		Name:    "test-sdk",
		Version: "1.0.0",
	}

	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)

	if scheduler == nil {
		t.Fatal("Expected non-nil scheduler")
	}

	if len(scheduler.buffers) != 1 {
		t.Errorf("Expected 1 buffer, got %d", len(scheduler.buffers))
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
		setupBuffers  func() map[ratelimit.Category]Storage[protocol.EnvelopeItem]
		addItems      func(buffers map[ratelimit.Category]Storage[protocol.EnvelopeItem])
		expectedCount int64
	}{
		{
			name: "single category with multiple items",
			setupBuffers: func() map[ratelimit.Category]Storage[protocol.EnvelopeItem] {
				return map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Storage[protocol.EnvelopeItem]) {
				for i := 1; i <= 5; i++ {
					item := protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"test"}`))
					buffers[ratelimit.CategoryError].Offer(*item)
				}
			},
			expectedCount: 5,
		},
		{
			name: "empty buffers",
			setupBuffers: func() map[ratelimit.Category]Storage[protocol.EnvelopeItem] {
				return map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
				}
			},
			addItems: func(_ map[ratelimit.Category]Storage[protocol.EnvelopeItem]) {
			},
			expectedCount: 0,
		},
		{
			name: "multiple categories",
			setupBuffers: func() map[ratelimit.Category]Storage[protocol.EnvelopeItem] {
				return map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
					ratelimit.CategoryError:       NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryTransaction: NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryMonitor:     NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryMonitor, 10, OverflowPolicyDropOldest, 1, 0),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Storage[protocol.EnvelopeItem]) {
				i := 0
				for category, buffer := range buffers {
					itemType := protocol.EnvelopeItemTypeEvent
					if category == ratelimit.CategoryTransaction {
						itemType = protocol.EnvelopeItemTypeTransaction
					}
					if category == ratelimit.CategoryMonitor {
						itemType = protocol.EnvelopeItemTypeCheckIn
					}
					item := protocol.NewEnvelopeItem(itemType, []byte(`{}`))
					buffer.Offer(*item)
					i++
				}
			},
			expectedCount: 3,
		},
		{
			name: "priority ordering - error and log",
			setupBuffers: func() map[ratelimit.Category]Storage[protocol.EnvelopeItem] {
				return map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
					ratelimit.CategoryLog:   NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Storage[protocol.EnvelopeItem]) {
				buffers[ratelimit.CategoryError].Offer(*protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{}`)))
				buffers[ratelimit.CategoryLog].Offer(*protocol.NewLogItem(1, []byte(`{"items":[{"level":"info","body":"log"}]}`)))
			},
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &testutils.MockTelemetryTransport{}
			dsn := &protocol.Dsn{}
			sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

			buffers := tt.setupBuffers()
			scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)

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

	buffer := NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
		ratelimit.CategoryError: buffer,
	}
	// no log buffer used in simplified scheduler tests
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)

	transport.SetRateLimited("error", true)

	scheduler.Start()
	defer scheduler.Stop(100 * time.Millisecond)

	item := protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"test"}`))
	buffer.Offer(*item)
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

	buffer := NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
		ratelimit.CategoryError: buffer,
	}
	// no log buffer used in simplified scheduler tests
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)

	scheduler.Start()
	scheduler.Start()

	item := protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"test"}`))
	buffer.Offer(*item)
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

	buffer := NewRingBuffer[protocol.EnvelopeItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	buffers := map[ratelimit.Category]Storage[protocol.EnvelopeItem]{
		ratelimit.CategoryError: buffer,
	}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo)

	scheduler.Start()

	for i := 1; i <= 5; i++ {
		item := protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"test"}`))
		buffer.Offer(*item)
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
