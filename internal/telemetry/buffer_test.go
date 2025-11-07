package telemetry

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type testItem struct {
	id   int
	data string
}

func TestNewRingBuffer(t *testing.T) {
	t.Run("with valid capacity", func(t *testing.T) {
		buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 50, OverflowPolicyDropOldest, 1, 0)
		if buffer.Capacity() != 50 {
			t.Errorf("Expected capacity 50, got %d", buffer.Capacity())
		}
		if buffer.Category() != ratelimit.CategoryError {
			t.Errorf("Expected category error, got %s", buffer.Category())
		}
		if buffer.Priority() != ratelimit.PriorityCritical {
			t.Errorf("Expected priority critical, got %s", buffer.Priority())
		}
	})

	t.Run("with zero capacity", func(t *testing.T) {
		buffer := NewRingBuffer[*testItem](ratelimit.CategoryLog, 0, OverflowPolicyDropOldest, 1, 0)
		if buffer.Capacity() != 100 {
			t.Errorf("Expected default capacity 100, got %d", buffer.Capacity())
		}
	})

	t.Run("with negative capacity", func(t *testing.T) {
		buffer := NewRingBuffer[*testItem](ratelimit.CategoryLog, -10, OverflowPolicyDropOldest, 1, 0)
		if buffer.Capacity() != 100 {
			t.Errorf("Expected default capacity 100, got %d", buffer.Capacity())
		}
	})
}

func TestBufferBasicOperations(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)

	// Test empty buffer
	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty initially")
	}
	if buffer.IsFull() {
		t.Error("Expected buffer to not be full initially")
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected size 0, got %d", buffer.Size())
	}

	// Test offering items
	item1 := &testItem{id: 1, data: "first"}
	if !buffer.Offer(item1) {
		t.Error("Expected successful offer")
	}
	if buffer.Size() != 1 {
		t.Errorf("Expected size 1, got %d", buffer.Size())
	}
	if buffer.IsEmpty() {
		t.Error("Expected buffer to not be empty")
	}

	item2 := &testItem{id: 2, data: "second"}
	item3 := &testItem{id: 3, data: "third"}
	buffer.Offer(item2)
	buffer.Offer(item3)

	if !buffer.IsFull() {
		t.Error("Expected buffer to be full")
	}
	if buffer.Size() != 3 {
		t.Errorf("Expected size 3, got %d", buffer.Size())
	}
}

func TestBufferPollOperation(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)

	// Test polling from empty buffer
	item, ok := buffer.Poll()
	if ok {
		t.Error("Expected poll to fail on empty buffer")
	}
	if item != nil {
		t.Error("Expected nil item from empty buffer")
	}

	// Add items and poll them
	item1 := &testItem{id: 1, data: "first"}
	item2 := &testItem{id: 2, data: "second"}
	buffer.Offer(item1)
	buffer.Offer(item2)

	// Poll first item
	polled, ok := buffer.Poll()
	if !ok {
		t.Error("Expected successful poll")
	}
	if polled.id != 1 {
		t.Errorf("Expected first item (id=1), got id=%d", polled.id)
	}
	if buffer.Size() != 1 {
		t.Errorf("Expected size 1 after poll, got %d", buffer.Size())
	}

	// Poll second item
	polled, ok = buffer.Poll()
	if !ok {
		t.Error("Expected successful poll")
	}
	if polled.id != 2 {
		t.Errorf("Expected second item (id=2), got id=%d", polled.id)
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected size 0 after polling all items, got %d", buffer.Size())
	}
}

func TestBufferOverflow(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropOldest, 1, 0)

	// Fill buffer to capacity
	item1 := &testItem{id: 1, data: "first"}
	item2 := &testItem{id: 2, data: "second"}
	buffer.Offer(item1)
	buffer.Offer(item2)

	// Add one more item (should cause overflow)
	item3 := &testItem{id: 3, data: "third"}
	if !buffer.Offer(item3) {
		t.Error("Expected offer to succeed even on overflow")
	}

	// Buffer should still be full
	if !buffer.IsFull() {
		t.Error("Expected buffer to remain full after overflow")
	}

	// First item should be dropped, so polling should return item2 first
	polled, ok := buffer.Poll()
	if !ok {
		t.Error("Expected successful poll after overflow")
	}
	if polled.id != 2 {
		t.Errorf("Expected second item (id=2) after overflow, got id=%d", polled.id)
	}

	// Next poll should return the overflow item
	polled, ok = buffer.Poll()
	if !ok {
		t.Error("Expected successful poll")
	}
	if polled.id != 3 {
		t.Errorf("Expected third item (id=3), got id=%d", polled.id)
	}

	// Check that dropped count is recorded
	if buffer.DroppedCount() != 1 {
		t.Errorf("Expected 1 dropped item, got %d", buffer.DroppedCount())
	}
}

func TestBufferDrain(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 5, OverflowPolicyDropOldest, 1, 0)

	// Drain empty buffer
	items := buffer.Drain()
	if items != nil {
		t.Error("Expected nil when draining empty buffer")
	}

	// Add some items
	for i := 1; i <= 3; i++ {
		buffer.Offer(&testItem{id: i, data: "item"})
	}

	// Drain buffer
	items = buffer.Drain()
	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(items))
	}

	// Check items are in correct order
	for i, item := range items {
		if item.id != i+1 {
			t.Errorf("Expected item %d, got %d", i+1, item.id)
		}
	}

	// Buffer should be empty after drain
	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty after drain")
	}
	if buffer.Size() != 0 {
		t.Errorf("Expected size 0 after drain, got %d", buffer.Size())
	}
}

func TestBufferMetrics(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropOldest, 1, 0)

	// Initial metrics
	if buffer.OfferedCount() != 0 {
		t.Errorf("Expected 0 offered items initially, got %d", buffer.OfferedCount())
	}
	if buffer.DroppedCount() != 0 {
		t.Errorf("Expected 0 dropped items initially, got %d", buffer.DroppedCount())
	}

	// Offer some items
	buffer.Offer(&testItem{id: 1})
	buffer.Offer(&testItem{id: 2})
	buffer.Offer(&testItem{id: 3}) // This should cause a drop

	if buffer.OfferedCount() != 3 {
		t.Errorf("Expected 3 offered items, got %d", buffer.OfferedCount())
	}
	if buffer.DroppedCount() != 1 {
		t.Errorf("Expected 1 dropped item, got %d", buffer.DroppedCount())
	}
}

func TestBufferConcurrency(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 100, OverflowPolicyDropOldest, 1, 0)

	const numGoroutines = 10
	const itemsPerGoroutine = 50

	var wg sync.WaitGroup

	// Concurrent offers
	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < itemsPerGoroutine; j++ {
				item := &testItem{
					id:   goroutineID*itemsPerGoroutine + j,
					data: "concurrent",
				}
				buffer.Offer(item)
			}
		}(i)
	}

	wg.Wait()

	// Check that we received all items (buffer capacity is 100, so some should be dropped)
	totalOffered := numGoroutines * itemsPerGoroutine
	if buffer.OfferedCount() != int64(totalOffered) {
		t.Errorf("Expected %d offered items, got %d", totalOffered, buffer.OfferedCount())
	}

	// Concurrent polls
	polledItems := make(map[int]bool)
	var pollMutex sync.Mutex

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for {
				item, ok := buffer.Poll()
				if !ok {
					break
				}
				pollMutex.Lock()
				polledItems[item.id] = true
				pollMutex.Unlock()
			}
		}()
	}

	wg.Wait()

	// Buffer should be empty after polling
	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty after concurrent polling")
	}
}

func TestBufferDifferentCategories(t *testing.T) {
	testCases := []struct {
		category         ratelimit.Category
		expectedPriority ratelimit.Priority
	}{
		{ratelimit.CategoryError, ratelimit.PriorityCritical},
		{ratelimit.CategoryMonitor, ratelimit.PriorityHigh},
		{ratelimit.CategoryLog, ratelimit.PriorityLow},
		{ratelimit.CategoryTransaction, ratelimit.PriorityMedium},
	}

	for _, tc := range testCases {
		t.Run(string(tc.category), func(t *testing.T) {
			buffer := NewRingBuffer[*testItem](tc.category, 10, OverflowPolicyDropOldest, 1, 0)
			if buffer.Category() != tc.category {
				t.Errorf("Expected category %s, got %s", tc.category, buffer.Category())
			}
			if buffer.Priority() != tc.expectedPriority {
				t.Errorf("Expected priority %s, got %s", tc.expectedPriority, buffer.Priority())
			}
		})
	}
}

func TestBufferStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 1000, OverflowPolicyDropOldest, 1, 0)

	const duration = 100 * time.Millisecond
	const numProducers = 5
	const numConsumers = 3

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Start producers
	wg.Add(numProducers)
	for i := 0; i < numProducers; i++ {
		go func(producerID int) {
			defer wg.Done()
			itemID := 0
			for {
				select {
				case <-stop:
					return
				default:
					item := &testItem{
						id:   producerID*10000 + itemID,
						data: "stress",
					}
					buffer.Offer(item)
					itemID++
				}
			}
		}(i)
	}

	// Start consumers
	wg.Add(numConsumers)
	var consumedCount int64
	for i := 0; i < numConsumers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					// Drain remaining items
					for {
						_, ok := buffer.Poll()
						if !ok {
							break
						}
						atomic.AddInt64(&consumedCount, 1)
					}
					return
				default:
					_, ok := buffer.Poll()
					if ok {
						atomic.AddInt64(&consumedCount, 1)
					}
				}
			}
		}()
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stop)
	wg.Wait()

	t.Logf("Stress test results: offered=%d, dropped=%d, consumed=%d",
		buffer.OfferedCount(), buffer.DroppedCount(), atomic.LoadInt64(&consumedCount))

	// Basic sanity checks
	if buffer.OfferedCount() <= 0 {
		t.Error("Expected some items to be offered")
	}
	if atomic.LoadInt64(&consumedCount) <= 0 {
		t.Error("Expected some items to be consumed")
	}
}

func TestOverflowPolicyDropOldest(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropOldest, 1, 0)

	// Fill buffer to capacity
	item1 := &testItem{id: 1, data: "first"}
	item2 := &testItem{id: 2, data: "second"}

	if !buffer.Offer(item1) {
		t.Error("Expected first offer to succeed")
	}
	if !buffer.Offer(item2) {
		t.Error("Expected second offer to succeed")
	}

	// Test overflow - should drop oldest (item1) and keep newest (item3)
	item3 := &testItem{id: 3, data: "third"}
	if !buffer.Offer(item3) {
		t.Error("Expected third offer to succeed with drop oldest policy")
	}

	// Verify oldest was dropped and new item was added
	if buffer.Size() != 2 {
		t.Errorf("Expected size 2, got %d", buffer.Size())
	}
	if buffer.DroppedCount() != 1 {
		t.Errorf("Expected 1 dropped item, got %d", buffer.DroppedCount())
	}

	// Poll items and verify order (should get item2, then item3)
	polled1, ok1 := buffer.Poll()
	if !ok1 || polled1.id != 2 {
		t.Errorf("Expected to poll item2 (id=2), got id=%d", polled1.id)
	}

	polled2, ok2 := buffer.Poll()
	if !ok2 || polled2.id != 3 {
		t.Errorf("Expected to poll item3 (id=3), got id=%d", polled2.id)
	}
}

func TestOverflowPolicyDropNewest(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropNewest, 1, 0)

	// Fill buffer to capacity
	item1 := &testItem{id: 1, data: "first"}
	item2 := &testItem{id: 2, data: "second"}

	if !buffer.Offer(item1) {
		t.Error("Expected first offer to succeed")
	}
	if !buffer.Offer(item2) {
		t.Error("Expected second offer to succeed")
	}

	// Test overflow - should drop newest (item3) and keep existing items
	item3 := &testItem{id: 3, data: "third"}
	if buffer.Offer(item3) {
		t.Error("Expected third offer to fail with drop newest policy")
	}

	// Verify newest was dropped and existing items remain
	if buffer.Size() != 2 {
		t.Errorf("Expected size 2, got %d", buffer.Size())
	}
	if buffer.DroppedCount() != 1 {
		t.Errorf("Expected 1 dropped item, got %d", buffer.DroppedCount())
	}

	// Poll items and verify order (should get original items)
	polled1, ok1 := buffer.Poll()
	if !ok1 || polled1.id != 1 {
		t.Errorf("Expected to poll item1 (id=1), got id=%d", polled1.id)
	}

	polled2, ok2 := buffer.Poll()
	if !ok2 || polled2.id != 2 {
		t.Errorf("Expected to poll item2 (id=2), got id=%d", polled2.id)
	}
}

func TestBufferDroppedCallback(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropOldest, 1, 0)

	var droppedItems []*testItem
	var dropReasons []string

	// Set up dropped callback
	buffer.SetDroppedCallback(func(item *testItem, reason string) {
		droppedItems = append(droppedItems, item)
		dropReasons = append(dropReasons, reason)
	})

	// Fill buffer to capacity
	item1 := &testItem{id: 1, data: "first"}
	item2 := &testItem{id: 2, data: "second"}
	buffer.Offer(item1)
	buffer.Offer(item2)

	// Trigger overflow
	item3 := &testItem{id: 3, data: "third"}
	buffer.Offer(item3)

	// Verify callback was called
	if len(droppedItems) != 1 {
		t.Errorf("Expected 1 dropped item in callback, got %d", len(droppedItems))
	}
	if len(dropReasons) != 1 {
		t.Errorf("Expected 1 drop reason in callback, got %d", len(dropReasons))
	}

	if droppedItems[0].id != 1 {
		t.Errorf("Expected dropped item to be item1 (id=1), got id=%d", droppedItems[0].id)
	}
	if dropReasons[0] != "buffer_full_drop_oldest" {
		t.Errorf("Expected drop reason 'buffer_full_drop_oldest', got '%s'", dropReasons[0])
	}
}

func TestBufferPollBatch(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 5, OverflowPolicyDropOldest, 1, 0)

	// Add some items
	for i := 1; i <= 5; i++ {
		item := &testItem{id: i, data: "test"}
		buffer.Offer(item)
	}

	// Test polling batch of 3
	batch := buffer.PollBatch(3)
	if len(batch) != 3 {
		t.Errorf("Expected batch size 3, got %d", len(batch))
	}

	// Verify order and IDs
	for i := 0; i < 3; i++ {
		if batch[i].id != i+1 {
			t.Errorf("Expected batch[%d] to have id %d, got %d", i, i+1, batch[i].id)
		}
	}

	// Verify remaining size
	if buffer.Size() != 2 {
		t.Errorf("Expected remaining size 2, got %d", buffer.Size())
	}
}

func TestBufferPeek(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)

	// Test peek on empty buffer
	_, ok := buffer.Peek()
	if ok {
		t.Error("Expected peek to fail on empty buffer")
	}

	// Add an item and test peek
	item := &testItem{id: 1, data: "test"}
	buffer.Offer(item)

	peeked, ok := buffer.Peek()
	if !ok {
		t.Error("Expected peek to succeed")
	}
	if peeked.id != 1 {
		t.Errorf("Expected peeked item to have id 1, got %d", peeked.id)
	}

	// Verify peek doesn't remove item
	if buffer.Size() != 1 {
		t.Errorf("Expected size to remain 1 after peek, got %d", buffer.Size())
	}
}

func TestBufferAdvancedMetrics(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 2, OverflowPolicyDropOldest, 1, 0)

	// Test initial metrics
	metrics := buffer.GetMetrics()
	if metrics.Category != ratelimit.CategoryError {
		t.Errorf("Expected category error, got %s", metrics.Category)
	}
	if metrics.Capacity != 2 {
		t.Errorf("Expected capacity 2, got %d", metrics.Capacity)
	}
	if metrics.Size != 0 {
		t.Errorf("Expected size 0, got %d", metrics.Size)
	}
	if metrics.Utilization != 0.0 {
		t.Errorf("Expected utilization 0.0, got %f", metrics.Utilization)
	}

	// Add items and test metrics
	buffer.Offer(&testItem{id: 1, data: "test"})
	buffer.Offer(&testItem{id: 2, data: "test"})
	buffer.Offer(&testItem{id: 3, data: "test"}) // This should cause a drop

	metrics = buffer.GetMetrics()
	if metrics.Size != 2 {
		t.Errorf("Expected size 2, got %d", metrics.Size)
	}
	if metrics.Utilization != 1.0 {
		t.Errorf("Expected utilization 1.0, got %f", metrics.Utilization)
	}
	if metrics.OfferedCount != 3 {
		t.Errorf("Expected offered count 3, got %d", metrics.OfferedCount)
	}
	if metrics.DroppedCount != 1 {
		t.Errorf("Expected dropped count 1, got %d", metrics.DroppedCount)
	}
	if metrics.AcceptedCount != 2 {
		t.Errorf("Expected accepted count 2, got %d", metrics.AcceptedCount)
	}
	if metrics.DropRate != 1.0/3.0 {
		t.Errorf("Expected drop rate %f, got %f", 1.0/3.0, metrics.DropRate)
	}
}

func TestBufferClear(t *testing.T) {
	buffer := NewRingBuffer[*testItem](ratelimit.CategoryError, 3, OverflowPolicyDropOldest, 1, 0)

	// Add some items
	buffer.Offer(&testItem{id: 1, data: "test"})
	buffer.Offer(&testItem{id: 2, data: "test"})

	// Verify items are there
	if buffer.Size() != 2 {
		t.Errorf("Expected size 2 before clear, got %d", buffer.Size())
	}

	// Clear and verify
	buffer.Clear()
	if buffer.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", buffer.Size())
	}
	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty after clear")
	}
}

func TestOverflowPolicyString(t *testing.T) {
	testCases := []struct {
		policy   OverflowPolicy
		expected string
	}{
		{OverflowPolicyDropOldest, "drop_oldest"},
		{OverflowPolicyDropNewest, "drop_newest"},
		{OverflowPolicy(999), "unknown"},
	}

	for _, tc := range testCases {
		if got := tc.policy.String(); got != tc.expected {
			t.Errorf("Expected %s, got %s", tc.expected, got)
		}
	}
}

func TestBufferIsReadyToFlush(t *testing.T) {
	tests := []struct {
		name          string
		category      ratelimit.Category
		itemsToAdd    int
		waitTime      time.Duration
		expectedReady bool
		reason        string
	}{
		{
			name:          "logs - batch size reached",
			category:      ratelimit.CategoryLog,
			itemsToAdd:    100,
			waitTime:      0,
			expectedReady: true,
			reason:        "batch size of 100 reached",
		},
		{
			name:          "logs - batch size not reached",
			category:      ratelimit.CategoryLog,
			itemsToAdd:    50,
			waitTime:      0,
			expectedReady: false,
			reason:        "batch size of 100 not reached and no timeout",
		},
		{
			name:          "error - batch size of 1 reached",
			category:      ratelimit.CategoryError,
			itemsToAdd:    1,
			waitTime:      0,
			expectedReady: true,
			reason:        "batch size of 1 reached",
		},
		{
			name:          "empty buffer",
			category:      ratelimit.CategoryLog,
			itemsToAdd:    0,
			waitTime:      0,
			expectedReady: false,
			reason:        "buffer is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := 1
			timeout := time.Duration(0)
			if tt.category == ratelimit.CategoryLog {
				batchSize = 100
				timeout = 5 * time.Second
			}
			buffer := NewRingBuffer[*testItem](tt.category, 200, OverflowPolicyDropOldest, batchSize, timeout)

			for i := 0; i < tt.itemsToAdd; i++ {
				buffer.Offer(&testItem{id: i, data: "test"})
			}

			if tt.waitTime > 0 {
				time.Sleep(tt.waitTime)
			}

			ready := buffer.IsReadyToFlush()
			if ready != tt.expectedReady {
				t.Errorf("Expected IsReadyToFlush() to be %v (%s), got %v", tt.expectedReady, tt.reason, ready)
			}
		})
	}
}

func TestBufferPollIfReady(t *testing.T) {
	tests := []struct {
		name            string
		category        ratelimit.Category
		itemsToAdd      int
		simulateTimeout bool
		expectedItems   int
	}{
		{
			name:            "logs - batch size reached",
			category:        ratelimit.CategoryLog,
			itemsToAdd:      100,
			simulateTimeout: false,
			expectedItems:   100,
		},
		{
			name:            "logs - batch size not reached, no timeout",
			category:        ratelimit.CategoryLog,
			itemsToAdd:      50,
			simulateTimeout: false,
			expectedItems:   0,
		},
		{
			name:            "logs - batch size not reached, timeout exceeded",
			category:        ratelimit.CategoryLog,
			itemsToAdd:      50,
			simulateTimeout: true,
			expectedItems:   50,
		},
		{
			name:            "error - batch size of 1 reached",
			category:        ratelimit.CategoryError,
			itemsToAdd:      1,
			simulateTimeout: false,
			expectedItems:   1,
		},
		{
			name:            "empty buffer",
			category:        ratelimit.CategoryLog,
			itemsToAdd:      0,
			simulateTimeout: false,
			expectedItems:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batchSize := 1
			timeout := time.Duration(0)
			if tt.category == ratelimit.CategoryLog {
				batchSize = 100
				timeout = 5 * time.Second
			}
			buffer := NewRingBuffer[*testItem](tt.category, 200, OverflowPolicyDropOldest, batchSize, timeout)

			for i := 0; i < tt.itemsToAdd; i++ {
				buffer.Offer(&testItem{id: i, data: "test"})
			}

			if tt.simulateTimeout {
				buffer.mu.Lock()
				buffer.lastFlushTime = time.Now().Add(-6 * time.Second)
				buffer.mu.Unlock()
			}

			items := buffer.PollIfReady()

			if len(items) != tt.expectedItems {
				t.Errorf("Expected %d items, got %d", tt.expectedItems, len(items))
			}

			if len(items) > 0 {
				buffer.mu.RLock()
				timeSinceFlush := time.Since(buffer.lastFlushTime)
				buffer.mu.RUnlock()

				if timeSinceFlush > 100*time.Millisecond {
					t.Errorf("Expected lastFlushTime to be updated after polling, but it was %v ago", timeSinceFlush)
				}
			}
		})
	}
}
