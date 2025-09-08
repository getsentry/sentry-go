package telemetry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Mock types to avoid import cycles
type mockEvent struct {
	Message string
	Extra   map[string]interface{}
}

type mockLog struct {
	Body string
}

type mockCheckIn struct {
	MonitorSlug string
	Status      string
}

func TestNewBuffer(t *testing.T) {
	tests := []struct {
		name           string
		category       DataCategory
		capacity       int
		overflowPolicy OverflowPolicy
		expectedCap    int
	}{
		{
			name:           "normal capacity",
			category:       DataCategoryError,
			capacity:       100,
			overflowPolicy: OverflowPolicyDropOldest,
			expectedCap:    100,
		},
		{
			name:           "zero capacity should default",
			category:       DataCategoryLog,
			capacity:       0,
			overflowPolicy: OverflowPolicyDropNewest,
			expectedCap:    100,
		},
		{
			name:           "negative capacity should default",
			category:       DataCategoryTransaction,
			capacity:       -10,
			overflowPolicy: OverflowPolicyDropOldest,
			expectedCap:    100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buffer := NewBuffer[mockEvent](tt.category, tt.capacity, tt.overflowPolicy)

			if buffer.Category() != tt.category {
				t.Errorf("expected category %v, got %v", tt.category, buffer.Category())
			}
			if buffer.Capacity() != tt.expectedCap {
				t.Errorf("expected capacity %d, got %d", tt.expectedCap, buffer.Capacity())
			}
			if buffer.Priority() != tt.category.GetPriority() {
				t.Errorf("expected priority %v, got %v", tt.category.GetPriority(), buffer.Priority())
			}
			if !buffer.IsEmpty() {
				t.Error("new buffer should be empty")
			}
			if buffer.IsFull() {
				t.Error("new buffer should not be full")
			}
		})
	}
}

func TestBuffer_BasicOperations_Event(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 3, OverflowPolicyDropOldest)

	event1 := mockEvent{Message: "test event 1"}
	event2 := mockEvent{Message: "test event 2"}
	event3 := mockEvent{Message: "test event 3"}

	if !buffer.Offer(event1) {
		t.Error("should accept first item")
	}
	if buffer.Size() != 1 {
		t.Errorf("expected size 1, got %d", buffer.Size())
	}

	if !buffer.Offer(event2) {
		t.Error("should accept second item")
	}
	if !buffer.Offer(event3) {
		t.Error("should accept third item")
	}

	if !buffer.IsFull() {
		t.Error("buffer should be full")
	}
	if buffer.IsEmpty() {
		t.Error("buffer should not be empty")
	}

	if item, ok := buffer.Peek(); !ok || item.Message != "test event 1" {
		t.Errorf("expected peek to return first event, got %v, ok=%v", item, ok)
	}
	if buffer.Size() != 3 {
		t.Error("peek should not change size")
	}

	if item, ok := buffer.Poll(); !ok || item.Message != "test event 1" {
		t.Errorf("expected poll to return first event, got %v, ok=%v", item, ok)
	}
	if buffer.Size() != 2 {
		t.Errorf("expected size 2 after poll, got %d", buffer.Size())
	}
}

func TestBuffer_BasicOperations_Log(t *testing.T) {
	buffer := NewBuffer[mockLog](DataCategoryLog, 3, OverflowPolicyDropOldest)

	log1 := mockLog{Body: "log message 1"}
	log2 := mockLog{Body: "log message 2"}
	log3 := mockLog{Body: "log message 3"}

	if !buffer.Offer(log1) {
		t.Error("should accept first item")
	}
	if buffer.Size() != 1 {
		t.Errorf("expected size 1, got %d", buffer.Size())
	}

	if !buffer.Offer(log2) {
		t.Error("should accept second item")
	}
	if !buffer.Offer(log3) {
		t.Error("should accept third item")
	}

	if item, ok := buffer.Poll(); !ok || item.Body != "log message 1" {
		t.Errorf("expected poll to return first log, got %v, ok=%v", item, ok)
	}
}

func TestBuffer_BasicOperations_CheckIn(t *testing.T) {
	buffer := NewBuffer[mockCheckIn](DataCategoryCheckIn, 3, OverflowPolicyDropOldest)

	checkin1 := mockCheckIn{MonitorSlug: "monitor1", Status: "ok"}
	checkin2 := mockCheckIn{MonitorSlug: "monitor2", Status: "ok"}

	if !buffer.Offer(checkin1) {
		t.Error("should accept first item")
	}
	if !buffer.Offer(checkin2) {
		t.Error("should accept second item")
	}

	if item, ok := buffer.Poll(); !ok || item.MonitorSlug != "monitor1" {
		t.Errorf("expected poll to return first checkin, got %v, ok=%v", item, ok)
	}
}

func TestBuffer_OverflowPolicyDropOldest(t *testing.T) {
	buffer := NewBuffer[mockLog](DataCategoryLog, 2, OverflowPolicyDropOldest)

	var droppedItems []mockLog
	var dropReasons []string

	buffer.SetDroppedCallback(func(item mockLog, reason string) {
		droppedItems = append(droppedItems, item)
		dropReasons = append(dropReasons, reason)
	})

	log1 := mockLog{Body: "log message 1"}
	log2 := mockLog{Body: "log message 2"}
	log3 := mockLog{Body: "log message 3"}

	buffer.Offer(log1)
	buffer.Offer(log2)

	if !buffer.Offer(log3) {
		t.Error("should accept log3 and drop oldest")
	}

	if len(droppedItems) != 1 || droppedItems[0].Body != "log message 1" {
		t.Errorf("expected first log to be dropped, got %v", droppedItems)
	}
	if len(dropReasons) != 1 || dropReasons[0] != "buffer_full_drop_oldest" {
		t.Errorf("expected drop reason 'buffer_full_drop_oldest', got %v", dropReasons)
	}

	if item, ok := buffer.Poll(); !ok || item.Body != "log message 2" {
		t.Errorf("expected second log, got %v, ok=%v", item, ok)
	}
	if item, ok := buffer.Poll(); !ok || item.Body != "log message 3" {
		t.Errorf("expected third log, got %v, ok=%v", item, ok)
	}
}

func TestBuffer_OverflowPolicyDropNewest(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 2, OverflowPolicyDropNewest)

	var droppedItems []mockEvent
	buffer.SetDroppedCallback(func(item mockEvent, reason string) {
		droppedItems = append(droppedItems, item)
	})

	event1 := mockEvent{Message: "event 1"}
	event2 := mockEvent{Message: "event 2"}
	event3 := mockEvent{Message: "event 3"}

	buffer.Offer(event1)
	buffer.Offer(event2)

	if buffer.Offer(event3) {
		t.Error("should not accept event3 with drop newest policy")
	}

	if len(droppedItems) != 1 || droppedItems[0].Message != "event 3" {
		t.Errorf("expected third event to be dropped, got %v", droppedItems)
	}

	if item, ok := buffer.Poll(); !ok || item.Message != "event 1" {
		t.Errorf("expected first event, got %v, ok=%v", item, ok)
	}
	if item, ok := buffer.Poll(); !ok || item.Message != "event 2" {
		t.Errorf("expected second event, got %v, ok=%v", item, ok)
	}
}

func TestBuffer_PollBatch(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 5, OverflowPolicyDropOldest)

	for i := 1; i <= 5; i++ {
		event := mockEvent{Message: "event"}
		event.Extra = map[string]interface{}{"id": i}
		buffer.Offer(event)
	}

	items := buffer.PollBatch(10)
	if len(items) != 5 {
		t.Errorf("expected 5 items, got %d", len(items))
	}
	for i, item := range items {
		expectedID := i + 1
		if id, ok := item.Extra["id"].(int); !ok || id != expectedID {
			t.Errorf("expected item %d, got %v", expectedID, item.Extra["id"])
		}
	}

	if !buffer.IsEmpty() {
		t.Error("buffer should be empty after draining")
	}

	items = buffer.PollBatch(5)
	if items != nil {
		t.Errorf("expected nil from empty buffer, got %v", items)
	}

	buffer.Offer(mockEvent{Message: "test"})
	items = buffer.PollBatch(0)
	if items != nil {
		t.Error("expected nil for maxItems=0")
	}
	items = buffer.PollBatch(-1)
	if items != nil {
		t.Error("expected nil for negative maxItems")
	}
}

func TestBuffer_Drain(t *testing.T) {
	buffer := NewBuffer[mockCheckIn](DataCategoryCheckIn, 3, OverflowPolicyDropOldest)

	items := buffer.Drain()
	if items != nil {
		t.Errorf("expected nil from empty buffer, got %v", items)
	}

	checkin1 := mockCheckIn{MonitorSlug: "a"}
	checkin2 := mockCheckIn{MonitorSlug: "b"}
	checkin3 := mockCheckIn{MonitorSlug: "c"}

	buffer.Offer(checkin1)
	buffer.Offer(checkin2)
	buffer.Offer(checkin3)

	items = buffer.Drain()
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}

	expected := []string{"a", "b", "c"}
	for i, item := range items {
		if item.MonitorSlug != expected[i] {
			t.Errorf("expected %v, got %v", expected[i], item.MonitorSlug)
		}
	}

	if !buffer.IsEmpty() {
		t.Error("buffer should be empty after drain")
	}
}

func TestBuffer_Clear(t *testing.T) {
	buffer := NewBuffer[mockLog](DataCategoryLog, 3, OverflowPolicyDropOldest)

	log1 := mockLog{Body: "a"}
	log2 := mockLog{Body: "b"}
	log3 := mockLog{Body: "c"}

	buffer.Offer(log1)
	buffer.Offer(log2)
	buffer.Offer(log3)

	buffer.Clear()

	if !buffer.IsEmpty() {
		t.Error("buffer should be empty after clear")
	}
	if buffer.Size() != 0 {
		t.Errorf("expected size 0, got %d", buffer.Size())
	}

	if !buffer.Offer(mockLog{Body: "new"}) {
		t.Error("should accept items after clear")
	}
}

func TestBuffer_Metrics(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 2, OverflowPolicyDropOldest)

	if buffer.OfferedCount() != 0 {
		t.Error("initial offered count should be 0")
	}
	if buffer.DroppedCount() != 0 {
		t.Error("initial dropped count should be 0")
	}
	if buffer.AcceptedCount() != 0 {
		t.Error("initial accepted count should be 0")
	}
	if buffer.DropRate() != 0.0 {
		t.Error("initial drop rate should be 0")
	}

	event1 := mockEvent{Message: "event1"}
	event2 := mockEvent{Message: "event2"}
	event3 := mockEvent{Message: "event3"}

	buffer.Offer(event1)
	buffer.Offer(event2)
	buffer.Offer(event3)

	if buffer.OfferedCount() != 3 {
		t.Errorf("expected offered count 3, got %d", buffer.OfferedCount())
	}
	if buffer.DroppedCount() != 1 {
		t.Errorf("expected dropped count 1, got %d", buffer.DroppedCount())
	}
	if buffer.AcceptedCount() != 2 {
		t.Errorf("expected accepted count 2, got %d", buffer.AcceptedCount())
	}

	expectedDropRate := 1.0 / 3.0
	if abs(buffer.DropRate()-expectedDropRate) > 0.001 {
		t.Errorf("expected drop rate %.3f, got %.3f", expectedDropRate, buffer.DropRate())
	}
}

func TestBuffer_GetMetrics(t *testing.T) {
	buffer := NewBuffer[mockLog](DataCategoryLog, 5, OverflowPolicyDropOldest)

	log1 := mockLog{Body: "log1"}
	log2 := mockLog{Body: "log2"}

	buffer.Offer(log1)
	buffer.Offer(log2)

	metrics := buffer.GetMetrics()

	if metrics.Category != DataCategoryLog {
		t.Errorf("expected category %v, got %v", DataCategoryLog, metrics.Category)
	}
	if metrics.Priority != PriorityMedium {
		t.Errorf("expected priority %v, got %v", PriorityMedium, metrics.Priority)
	}
	if metrics.Capacity != 5 {
		t.Errorf("expected capacity 5, got %d", metrics.Capacity)
	}
	if metrics.Size != 2 {
		t.Errorf("expected size 2, got %d", metrics.Size)
	}
	if metrics.OfferedCount != 2 {
		t.Errorf("expected offered count 2, got %d", metrics.OfferedCount)
	}
}

func TestBuffer_GetDiagnostics(t *testing.T) {
	buffer := NewBuffer[mockCheckIn](DataCategoryCheckIn, 2, OverflowPolicyDropNewest)

	checkin1 := mockCheckIn{MonitorSlug: "monitor1"}
	checkin2 := mockCheckIn{MonitorSlug: "monitor2"}

	buffer.Offer(checkin1)
	buffer.Offer(checkin2)

	diagnostics := buffer.GetDiagnostics()

	if diagnostics.OverflowPolicy != "drop_newest" {
		t.Errorf("expected overflow policy 'drop_newest', got %s", diagnostics.OverflowPolicy)
	}
	if diagnostics.IsEmpty {
		t.Error("diagnostics should show buffer is not empty")
	}
	if !diagnostics.IsFull {
		t.Error("diagnostics should show buffer is full")
	}
}

func TestBuffer_ConcurrentAccess(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 100, OverflowPolicyDropOldest)

	const numWriters = 5
	const numReaders = 3
	const itemsPerWriter = 50
	const testDuration = 100 * time.Millisecond

	var wg sync.WaitGroup
	done := make(chan struct{})

	go func() {
		time.Sleep(testDuration)
		close(done)
	}()

	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < itemsPerWriter; j++ {
				select {
				case <-done:
					return
				default:
					event := mockEvent{Message: "test event"}
					event.Extra = map[string]interface{}{"writer": id, "item": j}
					buffer.Offer(event)
				}
			}
		}(i)
	}

	polledCount := int64(0)
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					if _, ok := buffer.Poll(); ok {
						atomic.AddInt64(&polledCount, 1)
					} else {
						time.Sleep(1 * time.Millisecond)
					}
				}
			}
		}()
	}

	writersDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(writersDone)
	}()

	select {
	case <-writersDone:
		time.Sleep(10 * time.Millisecond)
		close(done)
	case <-done:
	}

	wg.Wait()

	totalOffered := buffer.OfferedCount()
	finalPolledCount := atomic.LoadInt64(&polledCount)

	if totalOffered == 0 {
		t.Error("no items were offered")
	}
	if finalPolledCount == 0 {
		t.Error("no items were polled")
	}

	if finalPolledCount > totalOffered {
		t.Errorf("polled more items (%d) than offered (%d)", finalPolledCount, totalOffered)
	}

	t.Logf("Offered: %d, Polled: %d, Dropped: %d, Final buffer size: %d",
		totalOffered, finalPolledCount, buffer.DroppedCount(), buffer.Size())
}

func TestBuffer_EdgeCases(t *testing.T) {
	buffer := NewBuffer[mockEvent](DataCategoryError, 1, OverflowPolicyDropOldest)

	if _, ok := buffer.Poll(); ok {
		t.Error("expected false from empty buffer")
	}

	if _, ok := buffer.Peek(); ok {
		t.Error("expected false from empty buffer")
	}

	event1 := mockEvent{Message: "event1"}
	event2 := mockEvent{Message: "event2"}

	if !buffer.Offer(event1) {
		t.Error("should accept first item")
	}
	if !buffer.Offer(event2) {
		t.Error("should accept second item and drop first")
	}

	if item, ok := buffer.Poll(); !ok || item.Message != "event2" {
		t.Errorf("expected 'event2', got %v, ok=%v", item, ok)
	}
	if !buffer.IsEmpty() {
		t.Error("buffer should be empty")
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
