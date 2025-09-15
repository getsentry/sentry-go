package sentry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
)

// mockTelemetryTransport is a test implementation of TelemetryTransport
type mockTelemetryTransport struct {
	sentEnvelopes    []*Envelope
	rateLimited      map[string]bool
	sendError        error
	mu               sync.Mutex
	sendCount        int64
	rateLimitedCalls int64
}

func (m *mockTelemetryTransport) SendEnvelope(envelope *Envelope) error {
	atomic.AddInt64(&m.sendCount, 1)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendError != nil {
		return m.sendError
	}

	m.sentEnvelopes = append(m.sentEnvelopes, envelope)
	return nil
}

func (m *mockTelemetryTransport) IsRateLimited(category ratelimit.Category) bool {
	atomic.AddInt64(&m.rateLimitedCalls, 1)
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.rateLimited == nil {
		return false
	}
	return m.rateLimited[string(category)]
}

func (m *mockTelemetryTransport) GetSentEnvelopes() []*Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*Envelope, len(m.sentEnvelopes))
	copy(result, m.sentEnvelopes)
	return result
}

func (m *mockTelemetryTransport) SetRateLimited(category string, limited bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rateLimited == nil {
		m.rateLimited = make(map[string]bool)
	}
	m.rateLimited[category] = limited
}

func (m *mockTelemetryTransport) GetSendCount() int64 {
	return atomic.LoadInt64(&m.sendCount)
}

func (m *mockTelemetryTransport) GetRateLimitedCalls() int64 {
	return atomic.LoadInt64(&m.rateLimitedCalls)
}

func (m *mockTelemetryTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sentEnvelopes = nil
	m.rateLimited = nil
	atomic.StoreInt64(&m.sendCount, 0)
	atomic.StoreInt64(&m.rateLimitedCalls, 0)
}

// testTelemetryItem is a test implementation of EnvelopeConvertible for scheduler tests
type testTelemetryItem struct {
	id       int
	data     string
	envelope *Envelope
}

func (t *testTelemetryItem) ToEnvelope(dsn *Dsn) (*Envelope, error) {
	if t.envelope != nil {
		return t.envelope, nil
	}

	// Create a simple test envelope
	envelope := &Envelope{
		Header: &EnvelopeHeader{
			EventID: EventID(t.data),
		},
		Items: []*EnvelopeItem{
			{
				Header: &EnvelopeItemHeader{
					Type: EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "` + t.data + `"}`),
			},
		},
	}
	return envelope, nil
}

func TestNewTelemetryScheduler(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest),
		telemetry.DataCategoryLog:   telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryLog, 10, telemetry.OverflowPolicyDropOldest),
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

	// Verify weighted round-robin cycle creation
	if len(scheduler.currentCycle) == 0 {
		t.Error("Expected non-empty priority cycle")
	}

	// Should have more critical priority slots than others
	criticalCount := 0
	mediumCount := 0
	for _, priority := range scheduler.currentCycle {
		if priority == telemetry.PriorityCritical {
			criticalCount++
		} else if priority == telemetry.PriorityMedium {
			mediumCount++
		}
	}

	if criticalCount <= mediumCount {
		t.Errorf("Expected more critical priority slots (%d) than medium (%d)", criticalCount, mediumCount)
	}
}

func TestTelemetrySchedulerBasicOperation(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Add some items to the buffer
	item1 := &testTelemetryItem{id: 1, data: "test1"}
	item2 := &testTelemetryItem{id: 2, data: "test2"}
	buffer.Offer(item1)
	buffer.Offer(item2)

	// Start the scheduler
	scheduler.Start()
	defer scheduler.Stop(time.Second)

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

	// Items should be processed
	if transport.GetSendCount() < 1 {
		t.Errorf("Expected at least 1 item to be processed, got %d", transport.GetSendCount())
	}
}

func TestTelemetrySchedulerFlush(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Add items to buffer
	for i := 1; i <= 5; i++ {
		item := &testTelemetryItem{id: i, data: "test"}
		buffer.Offer(item)
	}

	// Flush should process all items immediately
	scheduler.Flush()

	if transport.GetSendCount() != 5 {
		t.Errorf("Expected 5 items to be processed, got %d", transport.GetSendCount())
	}

	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty after flush")
	}
}

func TestTelemetrySchedulerRateLimiting(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Set rate limiting for error category
	transport.SetRateLimited("error", true)

	// Add items to buffer
	item := &testTelemetryItem{id: 1, data: "test"}
	buffer.Offer(item)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop(time.Second)

	// Wait a bit for processing
	time.Sleep(200 * time.Millisecond)

	// Items should not be processed due to rate limiting
	if transport.GetSendCount() > 0 {
		t.Errorf("Expected 0 items to be processed due to rate limiting, got %d", transport.GetSendCount())
	}

	// Rate limit check should have been called
	if transport.GetRateLimitedCalls() == 0 {
		t.Error("Expected rate limit check to be called")
	}
}

func TestTelemetrySchedulerPriorityOrdering(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	// Create buffers for different priority categories
	errorBuffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)   // Critical
	logBuffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryLog, 10, telemetry.OverflowPolicyDropOldest)       // Medium
	replayBuffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryReplay, 10, telemetry.OverflowPolicyDropOldest) // Lowest

	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError:  errorBuffer,
		telemetry.DataCategoryLog:    logBuffer,
		telemetry.DataCategoryReplay: replayBuffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Add one item to each buffer
	errorBuffer.Offer(&testTelemetryItem{id: 1, data: "error"})
	logBuffer.Offer(&testTelemetryItem{id: 2, data: "log"})
	replayBuffer.Offer(&testTelemetryItem{id: 3, data: "replay"})

	// Start scheduler and let it process a few cycles
	scheduler.Start()
	defer scheduler.Stop(time.Second)

	// Wait for processing
	time.Sleep(500 * time.Millisecond)

	// At least some items should be processed (scheduler uses round-robin)
	if transport.GetSendCount() < 1 {
		t.Errorf("Expected at least 1 item to be processed, got %d", transport.GetSendCount())
	}

	// Flush to ensure all remaining items are processed
	scheduler.Flush()

	// Now all items should be processed
	if transport.GetSendCount() != 3 {
		t.Errorf("Expected 3 items to be processed after flush, got %d", transport.GetSendCount())
	}
}

func TestTelemetrySchedulerStartStop(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Start and stop multiple times
	scheduler.Start()
	scheduler.Start() // Should be safe to call multiple times

	// Add an item
	item := &testTelemetryItem{id: 1, data: "test"}
	buffer.Offer(item)

	// Stop scheduler
	scheduler.Stop(time.Second)
	scheduler.Stop(time.Second) // Should be safe to call multiple times

	// Item should have been processed during shutdown
	if transport.GetSendCount() == 0 {
		t.Error("Expected at least 1 item to be processed")
	}
}

func TestTelemetrySchedulerEmptyBuffers(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	// Empty buffers
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest),
		telemetry.DataCategoryLog:   telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryLog, 10, telemetry.OverflowPolicyDropOldest),
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop(time.Second)

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// No items should be processed
	if transport.GetSendCount() != 0 {
		t.Errorf("Expected 0 items to be processed, got %d", transport.GetSendCount())
	}
}

func TestTelemetrySchedulerMultipleCategories(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	categories := []telemetry.DataCategory{
		telemetry.DataCategoryError, telemetry.DataCategoryTransaction, telemetry.DataCategorySession,
		telemetry.DataCategoryCheckIn, telemetry.DataCategoryLog, telemetry.DataCategorySpan,
		telemetry.DataCategoryProfile, telemetry.DataCategoryReplay, telemetry.DataCategoryFeedback,
	}

	buffers := make(map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible])
	for _, category := range categories {
		buffers[category] = telemetry.NewBuffer[EnvelopeConvertible](category, 10, telemetry.OverflowPolicyDropOldest)
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Add one item to each buffer
	for i, category := range categories {
		item := &testTelemetryItem{id: i + 1, data: string(category)}
		buffers[category].Offer(item)
	}

	// Flush to process all items
	scheduler.Flush()

	// All items should be processed
	if transport.GetSendCount() != int64(len(categories)) {
		t.Errorf("Expected %d items to be processed, got %d", len(categories), transport.GetSendCount())
	}
}

func TestTelemetrySchedulerContextCancellation(t *testing.T) {
	transport := &mockTelemetryTransport{}
	dsn := &Dsn{}

	buffer := telemetry.NewBuffer[EnvelopeConvertible](telemetry.DataCategoryError, 10, telemetry.OverflowPolicyDropOldest)
	buffers := map[telemetry.DataCategory]*telemetry.Buffer[EnvelopeConvertible]{
		telemetry.DataCategoryError: buffer,
	}

	scheduler := NewScheduler(buffers, transport, dsn)

	// Add items that won't be processed immediately
	for i := 1; i <= 5; i++ {
		item := &testTelemetryItem{id: i, data: "test"}
		buffer.Offer(item)
	}

	scheduler.Start()

	// Stop quickly to test cancellation
	done := make(chan struct{})
	go func() {
		defer close(done)
		scheduler.Stop(100 * time.Millisecond)
	}()

	// Should complete within reasonable time
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Scheduler stop took too long")
	}
}
