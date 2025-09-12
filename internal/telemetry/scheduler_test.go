package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// MockTransport implements the Transport interface for testing
type MockTransport struct {
	mu            sync.Mutex
	sentEnvelopes []*sentry.Envelope
	rateLimited   map[ratelimit.Category]bool
	sendDelay     time.Duration
	shouldError   bool
}

func NewMockTransport() *MockTransport {
	return &MockTransport{
		sentEnvelopes: make([]*sentry.Envelope, 0),
		rateLimited:   make(map[ratelimit.Category]bool),
	}
}

func (t *MockTransport) SendEnvelope(envelope *sentry.Envelope) error {
	if t.sendDelay > 0 {
		time.Sleep(t.sendDelay)
	}

	if t.shouldError {
		return &MockError{Message: "transport error"}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.sentEnvelopes = append(t.sentEnvelopes, envelope)
	return nil
}

func (t *MockTransport) IsRateLimited(category ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.rateLimited[category]
}

func (t *MockTransport) SetRateLimited(category ratelimit.Category, limited bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rateLimited[category] = limited
}

func (t *MockTransport) GetSentEnvelopes() []*sentry.Envelope {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]*sentry.Envelope, len(t.sentEnvelopes))
	copy(result, t.sentEnvelopes)
	return result
}

func (t *MockTransport) GetSentCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sentEnvelopes)
}

func (t *MockTransport) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sentEnvelopes = t.sentEnvelopes[:0]
}

// Configure implements Transport interface
func (t *MockTransport) Configure(options sentry.ClientOptions) {
	// No-op for mock
}

// Flush implements Transport interface
func (t *MockTransport) Flush(timeout time.Duration) bool {
	return true
}

// FlushWithContext implements Transport interface
func (t *MockTransport) FlushWithContext(ctx context.Context) bool {
	return true
}

// Close implements Transport interface
func (t *MockTransport) Close() {
	// No-op for mock
}

type MockError struct {
	Message string
}

func (e *MockError) Error() string {
	return e.Message
}

// Test data structures
type MockEvent struct {
	ID       string
	Type     string
	Data     string
	Category DataCategory
}

// Implement EnvelopeConvertible interface for MockEvent
func (e *MockEvent) ToEnvelope(dsn *sentry.Dsn, sentAt time.Time) (*sentry.Envelope, error) {
	event := sentry.NewEvent()
	event.Message = e.Data
	event.Type = e.Type
	return event.ToEnvelopeWithTime(dsn, sentAt)
}

func (e *MockEvent) GetCategory() DataCategory {
	return e.Category
}

func (e *MockEvent) GetPriority() Priority {
	return e.Category.GetPriority()
}

func (e *MockEvent) CanBatchWith(other EnvelopeConvertible) bool {
	return false // Mock events don't batch
}

func (e *MockEvent) BatchWith(other EnvelopeConvertible) EnvelopeConvertible {
	return e // Mock events don't batch, return self
}

// Helper function to create a test scheduler
func createTestScheduler(t *testing.T) (*EnvelopeScheduler, *MockTransport, context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	buffers := make(map[DataCategory]*Buffer[EnvelopeConvertible])
	buffers[DataCategoryError] = NewBuffer[EnvelopeConvertible](DataCategoryError, 10, OverflowPolicyDropOldest)
	buffers[DataCategoryLog] = NewBuffer[EnvelopeConvertible](DataCategoryLog, 10, OverflowPolicyDropOldest)

	transport := NewMockTransport()
	dsn, err := sentry.NewDsn("https://key@sentry.io/123")
	if err != nil {
		t.Fatalf("Failed to create DSN: %v", err)
	}
	config := DefaultTelemetryBufferConfig()

	scheduler := NewEnvelopeScheduler(ctx, buffers, transport, dsn, config)
	return scheduler, transport, ctx, cancel
}

func TestNewEnvelopeScheduler(t *testing.T) {
	ctx := context.Background()
	buffers := make(map[DataCategory]*Buffer[EnvelopeConvertible])
	buffers[DataCategoryError] = NewBuffer[EnvelopeConvertible](DataCategoryError, 10, OverflowPolicyDropOldest)

	transport := NewMockTransport()
	dsn, _ := sentry.NewDsn("https://key@sentry.io/123")
	config := DefaultTelemetryBufferConfig()

	scheduler := NewEnvelopeScheduler(ctx, buffers, transport, dsn, config)

	if scheduler == nil {
		t.Fatal("NewEnvelopeScheduler returned nil")
	}

	if scheduler.transport != transport {
		t.Error("Transport not set correctly")
	}

	if len(scheduler.buffers) != len(buffers) {
		t.Error("Buffers not set correctly")
	}
}

func TestEnvelopeScheduler_StartStop(t *testing.T) {
	scheduler, _, _, cancel := createTestScheduler(t)
	defer cancel()

	// Test start
	scheduler.Start()

	// Scheduler should be running (no direct way to check without diagnostics)

	// Test stop
	scheduler.Stop(1 * time.Second)

	// Scheduler should be stopped (no direct way to check without diagnostics)
}

func TestEnvelopeScheduler_ProcessEvents(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Add events to buffers
	errorEvent := &MockEvent{ID: "error1", Type: "error", Data: "test error", Category: DataCategoryError}
	logEvent := &MockEvent{ID: "log1", Type: "log", Data: "test log", Category: DataCategoryLog}

	scheduler.buffers[DataCategoryError].Offer(errorEvent)
	scheduler.buffers[DataCategoryLog].Offer(logEvent)

	// Start scheduler
	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Check that events were processed (entire buffers should be flushed)
	sentCount := transport.GetSentCount()
	if sentCount == 0 {
		t.Error("No envelopes were sent")
	}
}

func TestEnvelopeScheduler_PriorityWeighting(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Add events to both buffers
	for i := 0; i < 20; i++ {
		errorEvent := &MockEvent{ID: "error", Type: "error", Data: "test", Category: DataCategoryError}
		logEvent := &MockEvent{ID: "log", Type: "log", Data: "test", Category: DataCategoryLog}

		scheduler.buffers[DataCategoryError].Offer(errorEvent)
		scheduler.buffers[DataCategoryLog].Offer(logEvent)
	}

	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	// Let it process for a while
	time.Sleep(200 * time.Millisecond)

	// Priority weighting should result in buffers being processed based on round-robin
	// When a priority is selected, the entire buffer is flushed
	if transport.GetSentCount() == 0 {
		t.Error("No events were processed")
	}
}

func TestEnvelopeScheduler_BufferFlushing(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Add multiple events to a buffer
	for i := 0; i < 5; i++ {
		event := &MockEvent{ID: "log", Type: "log", Data: "test", Category: DataCategoryLog}
		scheduler.buffers[DataCategoryLog].Offer(event)
	}

	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// When the log buffer's priority is selected, the entire buffer should be flushed
	if transport.GetSentCount() == 0 {
		t.Error("Should have sent all events when buffer was flushed")
	}

	// Buffer should be empty after flushing
	if !scheduler.buffers[DataCategoryLog].IsEmpty() {
		t.Error("Buffer should be empty after being flushed")
	}
}

func TestEnvelopeScheduler_BatchSizeRespected(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Set small batch size for logs
	scheduler.config.BatchSizes[DataCategoryLog] = 2

	// Add multiple events that should be split into batches
	for i := 0; i < 5; i++ {
		event := &MockEvent{ID: "log", Type: "log", Data: "test", Category: DataCategoryLog}
		scheduler.buffers[DataCategoryLog].Offer(event)
	}

	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should have sent events (may be batched according to batch size)
	if transport.GetSentCount() == 0 {
		t.Error("Should have sent batched events")
	}
}

func TestEnvelopeScheduler_RateLimiting(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Set rate limiting for error category
	transport.SetRateLimited(ratelimit.CategoryError, true)

	// Add event
	event := &MockEvent{ID: "error", Type: "error", Data: "test", Category: DataCategoryError}
	scheduler.buffers[DataCategoryError].Offer(event)

	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should not have sent due to rate limiting
	if transport.GetSentCount() > 0 {
		t.Error("Should not send when rate limited")
	}

	// Remove rate limiting
	transport.SetRateLimited(ratelimit.CategoryError, false)

	// Add another event
	event2 := &MockEvent{ID: "error2", Type: "error", Data: "test", Category: DataCategoryError}
	scheduler.buffers[DataCategoryError].Offer(event2)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Should send when not rate limited
	if transport.GetSentCount() == 0 {
		t.Error("Should send when not rate limited")
	}
}

func TestEnvelopeScheduler_GracefulShutdown(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()

	// Add events to buffer
	for i := 0; i < 3; i++ {
		event := &MockEvent{ID: "log", Type: "log", Data: "test", Category: DataCategoryLog}
		scheduler.buffers[DataCategoryLog].Offer(event)
	}

	scheduler.Start()

	// Don't wait long enough for normal processing
	time.Sleep(10 * time.Millisecond)

	// Stop scheduler immediately - should flush remaining buffer items
	scheduler.Stop(1 * time.Second)

	// Should have sent all buffer events during shutdown flush
	if transport.GetSentCount() == 0 {
		t.Error("Should have flushed remaining buffer events during shutdown")
	}

	// Buffers should be empty after shutdown flush
	if !scheduler.buffers[DataCategoryLog].IsEmpty() {
		t.Error("Buffer should be empty after shutdown flush")
	}
}

func TestEnvelopeScheduler_ConcurrentAccess(t *testing.T) {
	scheduler, transport, _, cancel := createTestScheduler(t)
	defer cancel()
	scheduler.Start()
	defer scheduler.Stop(1 * time.Second)

	const numGoroutines = 5
	const eventsPerGoroutine = 20

	var wg sync.WaitGroup

	// Start multiple goroutines adding events
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < eventsPerGoroutine; j++ {
				category := DataCategoryError
				if j%2 == 0 {
					category = DataCategoryLog
				}

				event := &MockEvent{
					ID:       "event",
					Type:     string(category),
					Data:     "test",
					Category: category,
				}

				scheduler.buffers[category].Offer(event)
			}
		}(i)
	}

	wg.Wait()

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Should have processed some events
	if transport.GetSentCount() == 0 {
		t.Error("Should have processed some events in concurrent test")
	}

	// Scheduler should still be running (no direct way to check without diagnostics)
}
