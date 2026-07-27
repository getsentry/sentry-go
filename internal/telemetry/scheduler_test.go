package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	reportpkg "github.com/getsentry/sentry-go/report"
)

// fakeSpotlightSender is a test double for SpotlightSender.
type fakeSpotlightSender struct {
	mu         sync.Mutex
	sends      []*protocol.Envelope
	flushCalls int
}

// Send snapshots envelope.Items at call time, mirroring the real
// spotlightEnvelopeSender, which clones synchronously before returning. This
// matters for tests that check Send is called before the envelope is later
// mutated elsewhere - storing the raw pointer would silently observe later
// mutations too and defeat the point of the test.
func (f *fakeSpotlightSender) Send(envelope *protocol.Envelope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	snapshot := &protocol.Envelope{
		Header: envelope.Header,
		Items:  append([]*protocol.EnvelopeItem(nil), envelope.Items...),
	}
	f.sends = append(f.sends, snapshot)
}

func (f *fakeSpotlightSender) FlushWithContext(context.Context) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.flushCalls++
	return true
}

func (f *fakeSpotlightSender) Close() {}

func (f *fakeSpotlightSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sends)
}

func (f *fakeSpotlightSender) flushCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.flushCalls
}

type testTelemetryItem struct {
	id       int
	data     string
	category ratelimit.Category
}

func (t *testTelemetryItem) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	payload := `{"message": "` + t.data + `"}`
	return &protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type: protocol.EnvelopeItemTypeEvent,
		},
		Payload: []byte(payload),
	}, nil
}

func (t *testTelemetryItem) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	item, err := t.ToEnvelopeItem()
	if err != nil {
		return nil, err
	}
	return protocol.NewEnvelope(header, item), nil
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

func (t *testTelemetryItem) MakeSerializationSafe() {}

type failingTransactionTelemetryItem struct {
	testTelemetryItem
	spanCount int
}

func (f *failingTransactionTelemetryItem) ToEnvelope(_ *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	return nil, errors.New("boom")
}

func (f *failingTransactionTelemetryItem) GetSpanCount() int {
	return f.spanCount
}

func TestNewTelemetryScheduler(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
	}

	sdkInfo := &protocol.SdkInfo{
		Name:    "test-sdk",
		Version: "1.0.0",
	}

	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, nil)

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
		setupBuffers  func() map[ratelimit.Category]Buffer[protocol.TelemetryItem]
		addItems      func(buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem])
		expectedCount int64
	}{
		{
			name: "single category with multiple items",
			setupBuffers: func() map[ratelimit.Category]Buffer[protocol.TelemetryItem] {
				return map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem]) {
				for i := 1; i <= 5; i++ {
					buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: i, data: "test"})
				}
			},
			expectedCount: 5,
		},
		{
			name: "empty buffers",
			setupBuffers: func() map[ratelimit.Category]Buffer[protocol.TelemetryItem] {
				return map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(_ map[ratelimit.Category]Buffer[protocol.TelemetryItem]) {
			},
			expectedCount: 0,
		},
		{
			name: "multiple categories",
			setupBuffers: func() map[ratelimit.Category]Buffer[protocol.TelemetryItem] {
				return map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
					ratelimit.CategoryError:       NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryTransaction: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryMonitor:     NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryMonitor, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem]) {
				i := 0
				for category, buffer := range buffers {
					buffer.Offer(&testTelemetryItem{id: i + 1, data: string(category), category: category})
					i++
				}
			},
			expectedCount: 3,
		},
		{
			name: "priority ordering - error and log",
			setupBuffers: func() map[ratelimit.Category]Buffer[protocol.TelemetryItem] {
				return map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
					ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryLog:   NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem]) {
				buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: 1, data: "error", category: ratelimit.CategoryError})
				// simulate a log item (will be batched)
				buffers[ratelimit.CategoryLog].Offer(&testTelemetryItem{id: 2, data: "log", category: ratelimit.CategoryLog})
			},
			expectedCount: 2,
		},
		{
			name: "priority ordering - error and metric",
			setupBuffers: func() map[ratelimit.Category]Buffer[protocol.TelemetryItem] {
				return map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
					ratelimit.CategoryError:       NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryTraceMetric: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryTraceMetric, 10, OverflowPolicyDropOldest, 100, 5*time.Second, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[protocol.TelemetryItem]) {
				buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: 1, data: "error", category: ratelimit.CategoryError})
				// simulate a metric item (will be batched)
				buffers[ratelimit.CategoryTraceMetric].Offer(&testTelemetryItem{id: 2, data: "metric", category: ratelimit.CategoryTraceMetric})
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
			scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, nil)

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

	buffer := NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: buffer,
	}
	// no log buffer used in simplified scheduler tests
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, nil)

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

	buffer := NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: buffer,
	}
	// no log buffer used in simplified scheduler tests
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, nil)

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

	buffer := NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: buffer,
	}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, nil)

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

func TestTelemetrySchedulerRecordsFullDiscardCountsOnEnvelopeError(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	recorder := reportpkg.NewAggregator()

	buffer := NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryTransaction: buffer,
	}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, recorder, nil)

	buffer.Offer(&failingTransactionTelemetryItem{
		testTelemetryItem: testTelemetryItem{data: "tx", category: ratelimit.CategoryTransaction},
		spanCount:         3,
	})

	scheduler.Flush(time.Second)

	clientReport := recorder.TakeReport()
	if clientReport == nil {
		t.Fatal("expected client report")
	}

	outcomes := map[ratelimit.Category]int64{}
	for _, discarded := range clientReport.DiscardedEvents {
		if discarded.Reason != reportpkg.ReasonInternalError {
			t.Fatalf("unexpected reason: %s", discarded.Reason)
		}
		outcomes[discarded.Category] += discarded.Quantity
	}

	if outcomes[ratelimit.CategoryTransaction] != 1 {
		t.Fatalf("expected one discarded transaction, got %d", outcomes[ratelimit.CategoryTransaction])
	}
	if outcomes[ratelimit.CategorySpan] != 3 {
		t.Fatalf("expected discarded span count to be recorded, got %d", outcomes[ratelimit.CategorySpan])
	}
}

func TestSchedulerForwardsToSpotlight(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}
	spotlight := &fakeSpotlightSender{}

	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
	}
	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, spotlight)

	for i := 1; i <= 3; i++ {
		scheduler.Add(&testTelemetryItem{id: i, data: "test"})
	}

	if !scheduler.Flush(time.Second) {
		t.Fatalf("Expected Flush to succeed")
	}

	if got := spotlight.count(); got != 3 {
		t.Errorf("Expected 3 envelopes forwarded to Spotlight, got %d", got)
	}
	if got := transport.GetSendCount(); got != 3 {
		t.Errorf("Expected 3 envelopes sent to the real transport too, got %d", got)
	}
}

// TestSchedulerFlushWaitsForPendingBeforeCheckingSpotlight is a regression
// test: Add() can race with the background run() goroutine picking up and
// dispatching the item concurrently with a subsequent FlushWithContext call.
// FlushWithContext must still observe the resulting Spotlight send in every
// iteration, not intermittently return true because the sender's internal
// state was briefly (but not yet accurately) zero.
func TestSchedulerFlushWaitsForPendingBeforeCheckingSpotlight(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}
	spotlight := &fakeSpotlightSender{}

	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
	}
	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, spotlight)
	scheduler.Start()
	defer scheduler.Stop(time.Second)

	const iterations = 50
	for i := 0; i < iterations; i++ {
		scheduler.Add(&testTelemetryItem{id: i, data: "test"})
		if !scheduler.FlushWithContext(context.Background()) {
			t.Fatalf("Expected FlushWithContext to succeed on iteration %d", i)
		}
	}

	if got := spotlight.count(); got != iterations {
		t.Errorf("Expected all %d envelopes to have reached Spotlight by the time their Flush returned, got %d", iterations, got)
	}
}

// TestSchedulerFlushAlwaysCallsSpotlightFlush is a regression test: the
// return statement in FlushWithContext used to be
// `pendingOK && s.spotlight.FlushWithContext(ctx) && transportOK`, which
// short-circuits and skips calling spotlight.FlushWithContext entirely
// whenever pendingOK is already false (e.g. the pending count didn't reach
// zero before ctx timed out). That meant in-flight Spotlight sends were
// never waited on in exactly the situation where waiting mattered most.
func TestSchedulerFlushAlwaysCallsSpotlightFlush(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}
	spotlight := &fakeSpotlightSender{}

	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
	}
	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, spotlight)

	// Force pendingOK to be false by inflating the counter without ever
	// decrementing it, simulating a stuck/leaked pending item.
	scheduler.pending.Add(1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if scheduler.FlushWithContext(ctx) {
		t.Errorf("Expected FlushWithContext to report failure when pending never reaches zero")
	}
	if spotlight.flushCallCount() != 1 {
		t.Errorf("Expected spotlight.FlushWithContext to be called even when pendingOK is false, got %d calls", spotlight.flushCallCount())
	}
}

// mutatingTransport simulates AsyncTransport handing an envelope to a
// background worker that later mutates envelope.Items (e.g. AttachToEnvelope
// appending a client report), but does it synchronously for test determinism.
type mutatingTransport struct {
	testutils.MockTelemetryTransport
}

func (m *mutatingTransport) SendEnvelope(envelope *protocol.Envelope) error {
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeClientReport},
		Payload: []byte(`{"discarded_events":[]}`),
	})
	return m.MockTelemetryTransport.SendEnvelope(envelope)
}

// TestSchedulerClonesForSpotlightBeforeSendingToTransport is a regression
// test: sendItem used to call transport.SendEnvelope before spotlight.Send,
// so the transport's background worker could start mutating envelope.Items
// (e.g. attaching a client report) concurrently with spotlight.Send's
// synchronous clone reading that same slice - a data race. spotlight.Send
// must be called (and its clone taken) before the envelope is ever handed to
// the transport.
func TestSchedulerClonesForSpotlightBeforeSendingToTransport(t *testing.T) {
	transport := &mutatingTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}
	spotlight := &fakeSpotlightSender{}

	buffers := map[ratelimit.Category]Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: NewRingBuffer[protocol.TelemetryItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
	}
	scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil, spotlight)

	scheduler.Add(&testTelemetryItem{id: 1, data: "test"})
	if !scheduler.Flush(time.Second) {
		t.Fatalf("Expected Flush to succeed")
	}

	if spotlight.count() != 1 {
		t.Fatalf("Expected 1 envelope forwarded to Spotlight, got %d", spotlight.count())
	}
	for _, item := range spotlight.sends[0].Items {
		if item.Header.Type == protocol.EnvelopeItemTypeClientReport {
			t.Errorf("Expected Spotlight to receive the envelope before the transport's mutation, but found a client_report item")
		}
	}
}
