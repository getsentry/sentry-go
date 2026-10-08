package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	reportpkg "github.com/getsentry/sentry-go/report"
)

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

	buffers := map[ratelimit.Category]Buffer[Item]{
		ratelimit.CategoryError: NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
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
		setupBuffers  func() map[ratelimit.Category]Buffer[Item]
		addItems      func(buffers map[ratelimit.Category]Buffer[Item])
		expectedCount int64
	}{
		{
			name: "single category with multiple items",
			setupBuffers: func() map[ratelimit.Category]Buffer[Item] {
				return map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError: NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[Item]) {
				for i := 1; i <= 5; i++ {
					buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: i, data: "test"})
				}
			},
			expectedCount: 5,
		},
		{
			name: "empty buffers",
			setupBuffers: func() map[ratelimit.Category]Buffer[Item] {
				return map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError: NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(_ map[ratelimit.Category]Buffer[Item]) {
			},
			expectedCount: 0,
		},
		{
			name: "multiple categories",
			setupBuffers: func() map[ratelimit.Category]Buffer[Item] {
				return map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError:       NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryTransaction: NewRingBuffer[Item](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryMonitor:     NewRingBuffer[Item](ratelimit.CategoryMonitor, 10, OverflowPolicyDropOldest, 1, 0, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[Item]) {
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
			setupBuffers: func() map[ratelimit.Category]Buffer[Item] {
				return map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError: NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryLog:   NewRingBuffer[Item](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 5*time.Second, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[Item]) {
				buffers[ratelimit.CategoryError].Offer(&testTelemetryItem{id: 1, data: "error", category: ratelimit.CategoryError})
				// simulate a log item (will be batched)
				buffers[ratelimit.CategoryLog].Offer(&testTelemetryItem{id: 2, data: "log", category: ratelimit.CategoryLog})
			},
			expectedCount: 2,
		},
		{
			name: "priority ordering - error and metric",
			setupBuffers: func() map[ratelimit.Category]Buffer[Item] {
				return map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError:       NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil),
					ratelimit.CategoryTraceMetric: NewRingBuffer[Item](ratelimit.CategoryTraceMetric, 10, OverflowPolicyDropOldest, 100, 5*time.Second, nil),
				}
			},
			addItems: func(buffers map[ratelimit.Category]Buffer[Item]) {
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

type blockingTelemetryTransport struct {
	testutils.MockTelemetryTransport
	sendStarted chan struct{}
	resumeSend  chan struct{}
}

func (t *blockingTelemetryTransport) SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	close(t.sendStarted)
	<-t.resumeSend
	return t.MockTelemetryTransport.SendEnvelope(ctx, envelope)
}

func TestTelemetrySchedulerFlushWaitsForInFlightBatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		finish func(cancel context.CancelFunc, resume func())
		want   bool
	}{
		{"sent", func(_ context.CancelFunc, resume func()) { resume() }, true},
		{"canceled", func(cancel context.CancelFunc, _ func()) { cancel() }, false},
		{"timed out", func(_ context.CancelFunc, _ func()) { time.Sleep(time.Second) }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &blockingTelemetryTransport{
					sendStarted: make(chan struct{}),
					resumeSend:  make(chan struct{}),
				}
				buffer := NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
				scheduler := NewScheduler(map[ratelimit.Category]Buffer[Item]{
					ratelimit.CategoryError: buffer,
				}, transport, &protocol.Dsn{}, nil, nil, nil)
				scheduler.Start()
				resume := sync.OnceFunc(func() { close(transport.resumeSend) })
				t.Cleanup(func() {
					resume()
					scheduler.Stop(testutils.FlushTimeout())
				})

				require.True(t, scheduler.Add(&testTelemetryItem{data: "in-flight"}))
				<-transport.sendStarted
				require.True(t, buffer.IsEmpty())

				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				flushed := make(chan bool, 2)
				for range cap(flushed) {
					go func() { flushed <- scheduler.FlushWithContext(ctx) }()
				}
				synctest.Wait()
				require.Empty(t, flushed, "Flush returned before the batch reached the transport")

				tt.finish(cancel, resume)
				for range cap(flushed) {
					require.Equal(t, tt.want, <-flushed)
				}

				resume()
				require.True(t, scheduler.Flush(testutils.FlushTimeout()))
				require.Equal(t, int64(1), transport.GetSendCount())
			})
		})
	}
}

func TestTelemetrySchedulerFlushAlreadyCanceled(t *testing.T) {
	t.Parallel()

	transport := &testutils.MockTelemetryTransport{}
	scheduler := NewScheduler(nil, transport, &protocol.Dsn{}, nil, nil, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.False(t, scheduler.FlushWithContext(ctx))
	require.True(t, scheduler.Flush(testutils.FlushTimeout()))
}

func TestTelemetrySchedulerStartStop(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}

	buffer := NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[Item]{
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

	buffer := NewRingBuffer[Item](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0, nil)
	buffers := map[ratelimit.Category]Buffer[Item]{
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

func TestTelemetrySchedulerStopFlushesAcceptedItems(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	buffer := NewRingBuffer[Item](ratelimit.CategoryLog, 1<<20, OverflowPolicyDropNewest, 1<<20, time.Hour, nil)
	scheduler := NewScheduler(map[ratelimit.Category]Buffer[Item]{ratelimit.CategoryLog: buffer}, transport, &protocol.Dsn{}, nil, nil, nil)
	scheduler.Start()

	var accepted atomic.Int64
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for scheduler.Add(&testTelemetryItem{category: ratelimit.CategoryLog}) {
				accepted.Add(1)
			}
		})
	}
	for accepted.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	scheduler.Stop(testutils.FlushTimeout())
	wg.Wait()

	var sent int64
	for _, envelope := range transport.GetSentEnvelopes() {
		for _, item := range envelope.Items {
			sent += int64(*item.Header.ItemCount)
		}
	}
	require.Equal(t, accepted.Load(), sent, "every accepted item must be flushed")
	require.False(t, scheduler.Add(&testTelemetryItem{category: ratelimit.CategoryLog}), "Add after Stop must be rejected")
}

func TestTelemetrySchedulerClientReportDelivery(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name       string
		telemetry  bool
		pending    bool
		standalone bool
	}{
		{name: "empty envelope"},
		{name: "empty envelope preserves pending report", pending: true},
		{name: "telemetry without report", telemetry: true},
		{name: "telemetry with report", telemetry: true, pending: true},
		{name: "standalone without report", standalone: true},
		{name: "standalone with report", standalone: true, pending: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			transport := &testutils.MockTelemetryTransport{}
			recorder := reportpkg.NewAggregator()
			want := []reportpkg.DiscardedEvent{{Reason: reportpkg.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 2}}
			if tt.pending {
				recorder.Record(reportpkg.ReasonBeforeSend, ratelimit.CategoryError, 2)
			}
			scheduler := NewScheduler(nil, transport, &protocol.Dsn{}, nil, recorder, recorder)
			t.Cleanup(scheduler.cancel)

			var sent bool
			if tt.standalone {
				sent = scheduler.sendClientReport(context.Background())
			} else {
				envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{})
				if tt.telemetry {
					envelope.AddItem(protocol.NewTransactionItem(0, []byte(`{}`)))
				}
				sent = scheduler.sendEnvelope(context.Background(), envelope, false)
			}

			wantSent := tt.telemetry || (tt.standalone && tt.pending)
			require.Equal(t, wantSent, sent)
			envelopes := transport.GetSentEnvelopes()
			if !wantSent {
				require.Empty(t, envelopes)
				if tt.pending {
					pending := recorder.TakeReport()
					require.NotNil(t, pending)
					require.Equal(t, want, pending.DiscardedEvents)
				}
				return
			}

			require.Len(t, envelopes, 1)
			items := envelopes[0].Items
			if tt.telemetry {
				require.Equal(t, protocol.EnvelopeItemTypeTransaction, items[0].Header.Type)
				items = items[1:]
			}
			if tt.pending {
				require.Len(t, items, 1)
				require.Equal(t, protocol.EnvelopeItemTypeClientReport, items[0].Header.Type)
				var clientReport reportpkg.ClientReport
				require.NoError(t, json.Unmarshal(items[0].Payload, &clientReport))
				require.Equal(t, want, clientReport.DiscardedEvents)
			} else {
				require.Empty(t, items)
			}
			require.Nil(t, recorder.TakeReport())
		})
	}
}

type rejectingTransport struct {
	testutils.MockTelemetryTransport
	err error
}

func (t *rejectingTransport) SendEnvelope(context.Context, *protocol.Envelope) error { return t.err }

type transactionTelemetryItem struct{ testTelemetryItem }

func (t *transactionTelemetryItem) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	return protocol.NewEnvelope(header, protocol.NewTransactionItem(3, []byte(`{}`))), nil
}

func TestTelemetrySchedulerRecordsFullDiscardCountsOnEnvelopeError(t *testing.T) {
	transaction := testTelemetryItem{data: "tx", category: ratelimit.CategoryTransaction}
	tests := []struct {
		name   string
		item   Item
		err    error
		reason reportpkg.DiscardReason
	}{
		{"conversion error", &failingTransactionTelemetryItem{testTelemetryItem: transaction, spanCount: 3}, errors.New("report rejected"), reportpkg.ReasonInternalError},
		{"queue full", &transactionTelemetryItem{transaction}, ErrQueueFull, reportpkg.ReasonQueueOverflow},
		{"send error", &transactionTelemetryItem{transaction}, errors.New("send failed"), reportpkg.ReasonSendError},
		{"standalone report queue full", nil, ErrQueueFull, reportpkg.ReasonQueueOverflow},
		{"standalone report send error", nil, errors.New("send failed"), reportpkg.ReasonSendError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &rejectingTransport{err: tt.err}
			dsn := &protocol.Dsn{}
			recorder := reportpkg.NewAggregator()
			recorder.Record(reportpkg.ReasonBeforeSend, ratelimit.CategoryError, 2)

			buffer := NewRingBuffer[Item](ratelimit.CategoryTransaction, 10, OverflowPolicyDropOldest, 1, 0, nil)
			buffers := map[ratelimit.Category]Buffer[Item]{
				ratelimit.CategoryTransaction: buffer,
			}
			sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

			scheduler := NewScheduler(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, recorder, recorder)

			if tt.item != nil {
				buffer.Offer(tt.item)
			}
			require.True(t, scheduler.Flush(time.Second))

			clientReport := recorder.TakeReport()
			require.NotNil(t, clientReport)
			want := []reportpkg.DiscardedEvent{{Reason: reportpkg.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 2}}
			if tt.item != nil {
				want = append(want,
					reportpkg.DiscardedEvent{Reason: tt.reason, Category: ratelimit.CategoryTransaction, Quantity: 1},
					reportpkg.DiscardedEvent{Reason: tt.reason, Category: ratelimit.CategorySpan, Quantity: 3},
				)
			}
			require.ElementsMatch(t, want, clientReport.DiscardedEvents)
		})
	}
}
