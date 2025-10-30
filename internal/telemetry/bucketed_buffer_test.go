package telemetry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type tbItem struct {
	id    int
	trace string
}

func (i tbItem) GetTraceID() (string, bool) {
	if i.trace == "" {
		return "", false
	}
	return i.trace, true
}

func TestBucketedBufferPollOperation(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 3, 0)
	if !b.Offer(tbItem{id: 1}) || !b.Offer(tbItem{id: 2}) {
		t.Fatal("offer failed")
	}
	if b.Size() != 2 {
		t.Fatalf("size want 2 got %d", b.Size())
	}
	if it, ok := b.Poll(); !ok || it.id != 1 {
		t.Fatalf("poll got %#v ok=%v", it, ok)
	}
	if it, ok := b.Poll(); !ok || it.id != 2 {
		t.Fatalf("poll got %#v ok=%v", it, ok)
	}
	if !b.IsEmpty() {
		t.Fatal("expected empty after polls")
	}
}

func TestBucketedBufferOverflowDropOldest(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)
	dropped := 0
	b.SetDroppedCallback(func(_ tbItem, _ string) { dropped++ })
	b.Offer(tbItem{id: 1, trace: "a"})
	b.Offer(tbItem{id: 2, trace: "b"})
	b.Offer(tbItem{id: 3, trace: "c"})
	if !b.Offer(tbItem{id: 4, trace: "d"}) {
		t.Fatal("offer should succeed and drop oldest")
	}
	if dropped == 0 {
		t.Fatal("expected at least one dropped callback")
	}
	if b.Size() != 3 {
		t.Fatalf("size should remain at capacity, got %d", b.Size())
	}
}

func TestBucketedBufferPollIfReady_BatchSize(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 3, 0)
	for i := 1; i <= 3; i++ {
		b.Offer(tbItem{id: i, trace: "t"})
	}
	items := b.PollIfReady()
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if b.Size() != 0 {
		t.Fatalf("expected empty after PollIfReady, size %d", b.Size())
	}
}

func TestBucketedBufferPollIfReady_Timeout(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryLog, 10, OverflowPolicyDropOldest, 100, 1*time.Millisecond)
	b.Offer(tbItem{id: 1, trace: "t"})
	time.Sleep(3 * time.Millisecond)
	items := b.PollIfReady()
	if len(items) != 1 {
		t.Fatalf("expected 1 item due to timeout, got %d", len(items))
	}
}

func TestNewBucketedBuffer(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryLog, 0, OverflowPolicyDropOldest, 0, -1)
	if b.Capacity() != 100 {
		t.Fatalf("default capacity want 100 got %d", b.Capacity())
	}
	if b.Category() != ratelimit.CategoryLog {
		t.Fatalf("category mismatch: %v", b.Category())
	}
	if b.Priority() != ratelimit.CategoryLog.GetPriority() {
		t.Fatalf("priority mismatch: %v", b.Priority())
	}
}

func TestBucketedBufferBasicOperations(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	if !b.IsEmpty() || b.IsFull() || b.Size() != 0 {
		t.Fatalf("unexpected initial state: empty=%v full=%v size=%d", b.IsEmpty(), b.IsFull(), b.Size())
	}
	b.Offer(tbItem{id: 1, trace: "t1"})
	if it, ok := b.Peek(); !ok || it.id != 1 {
		t.Fatalf("peek got %#v ok=%v", it, ok)
	}
	// Same-trace aggregation
	for i := 2; i <= 3; i++ {
		b.Offer(tbItem{id: i, trace: "t1"})
	}
	batch := b.PollBatch(3)
	if len(batch) != 3 {
		t.Fatalf("batch len want 3 got %d", len(batch))
	}
}

func TestBucketedBufferPollBatchAcrossBuckets(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 10, 0)
	// Two buckets with different traces
	b.Offer(tbItem{id: 1, trace: "a"})
	b.Offer(tbItem{id: 2, trace: "a"})
	b.Offer(tbItem{id: 3, trace: "b"})
	b.Offer(tbItem{id: 4, trace: "b"})

	batch := b.PollBatch(3)
	if len(batch) != 3 {
		t.Fatalf("want 3 got %d", len(batch))
	}
}

func TestBucketedBufferDrain(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0)
	for i := 1; i <= 5; i++ {
		b.Offer(tbItem{id: i, trace: "t"})
	}
	items := b.Drain()
	if len(items) != 5 {
		t.Fatalf("want 5 got %d", len(items))
	}
	if !b.IsEmpty() || b.Size() != 0 {
		t.Fatalf("buffer not reset after drain, size=%d", b.Size())
	}
}

func TestBucketedBufferMetrics(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 10, OverflowPolicyDropNewest, 1, 0)
	if b.OfferedCount() != 0 || b.DroppedCount() != 0 {
		t.Fatalf("initial metrics not zero")
	}
	for i := 0; i < 12; i++ { // exceed capacity to force drops
		b.Offer(tbItem{id: i})
	}
	if b.OfferedCount() != 12 {
		t.Fatalf("offered want 12 got %d", b.OfferedCount())
	}
	if b.DroppedCount() == 0 {
		t.Fatalf("expected some drops with DropNewest policy")
	}
	if b.Utilization() <= 0 || b.Utilization() > 1 {
		t.Fatalf("unexpected utilization: %f", b.Utilization())
	}
}

func TestBucketedBufferOverflowDropNewest(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 2, OverflowPolicyDropNewest, 1, 0)
	b.Offer(tbItem{id: 1})
	b.Offer(tbItem{id: 2})
	if ok := b.Offer(tbItem{id: 3}); ok {
		t.Fatal("expected offer to fail with drop newest when full")
	}
}

func TestBucketedBufferDroppedCallback(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)
	calls := 0
	b.SetDroppedCallback(func(_ tbItem, reason string) {
		calls++
		if reason != "buffer_full_drop_oldest_bucket" && reason != "buffer_full_invalid_state" && reason != "buffer_full_drop_newest" {
			t.Fatalf("unexpected drop reason: %s", reason)
		}
	})
	// oldest bucket will have 2 items
	b.Offer(tbItem{id: 1, trace: "x"})
	b.Offer(tbItem{id: 2, trace: "x"})
	b.Offer(tbItem{id: 3, trace: "y"})
	// overflow should drop bucket with 2 items triggering 2 callbacks
	b.Offer(tbItem{id: 4, trace: "z"})
	if calls < 1 {
		t.Fatalf("expected at least one drop callback, got %d", calls)
	}
}

func TestBucketedBufferClear(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 5, OverflowPolicyDropOldest, 1, 0)
	b.Offer(tbItem{id: 1})
	b.Offer(tbItem{id: 2})
	b.Clear()
	if !b.IsEmpty() || b.Size() != 0 {
		t.Fatalf("expected empty after clear")
	}
}

func TestBucketedBufferIsReadyToFlush(t *testing.T) {
	tests := []struct {
		name     string
		category ratelimit.Category
		items    int
		timeout  time.Duration
		expect   bool
	}{
		{"logs batch reached", ratelimit.CategoryLog, 3, 0, true},
		{"logs batch not reached", ratelimit.CategoryLog, 2, 0, false},
		{"timeout reached", ratelimit.CategoryLog, 1, 2 * time.Millisecond, true},
		{"error batch 1", ratelimit.CategoryError, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := 3
			if tt.category == ratelimit.CategoryError {
				batch = 1
			}
			b := NewBucketedBuffer[tbItem](tt.category, 10, OverflowPolicyDropOldest, batch, tt.timeout)
			for i := 0; i < tt.items; i++ {
				b.Offer(tbItem{id: i, trace: "t"})
			}
			if tt.timeout > 0 {
				time.Sleep(tt.timeout + 1*time.Millisecond)
			}
			ready := b.IsReadyToFlush()
			if ready != tt.expect {
				t.Fatalf("ready=%v expect=%v", ready, tt.expect)
			}
		})
	}
}

func TestBucketedBufferConcurrency(t *testing.T) {
	b := NewBucketedBuffer[tbItem](ratelimit.CategoryError, 200, OverflowPolicyDropOldest, 1, 0)
	const producers = 5
	const per = 50
	var wg sync.WaitGroup
	wg.Add(producers)
	for p := 0; p < producers; p++ {
		go func(pid int) {
			defer wg.Done()
			for j := 0; j < per; j++ {
				b.Offer(tbItem{id: pid*per + j})
			}
		}(p)
	}
	wg.Wait()
	var polled int64
	for {
		if _, ok := b.Poll(); !ok {
			break
		}
		atomic.AddInt64(&polled, 1)
	}
	if polled == 0 {
		t.Fatal("expected to poll some items")
	}
}
