package telemetry

import (
	"sync"
	"sync/atomic"
	"time"
)

const defaultCapacity = 100

// Buffer is a thread-safe ring buffer with overflow policies.
type Buffer[T any] struct {
	mu       sync.RWMutex
	items    []T
	head     int
	tail     int
	size     int
	capacity int

	category       DataCategory
	priority       Priority
	overflowPolicy OverflowPolicy

	offered   int64
	dropped   int64
	onDropped func(item T, reason string)
}

func NewBuffer[T any](category DataCategory, capacity int, overflowPolicy OverflowPolicy) *Buffer[T] {
	if capacity <= 0 {
		capacity = defaultCapacity
	}

	return &Buffer[T]{
		items:          make([]T, capacity),
		capacity:       capacity,
		category:       category,
		priority:       category.GetPriority(),
		overflowPolicy: overflowPolicy,
	}
}

func (b *Buffer[T]) SetDroppedCallback(callback func(item T, reason string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDropped = callback
}

// Offer adds an item to the buffer, returns false if dropped due to overflow.
func (b *Buffer[T]) Offer(item T) bool {
	atomic.AddInt64(&b.offered, 1)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size < b.capacity {
		b.items[b.tail] = item
		b.tail = (b.tail + 1) % b.capacity
		b.size++
		return true
	}

	switch b.overflowPolicy {
	case OverflowPolicyDropOldest:
		oldItem := b.items[b.head]
		b.items[b.head] = item
		b.head = (b.head + 1) % b.capacity
		b.tail = (b.tail + 1) % b.capacity

		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(oldItem, "buffer_full_drop_oldest")
		}
		return true

	case OverflowPolicyDropNewest:
		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(item, "buffer_full_drop_newest")
		}
		return false

	default:
		atomic.AddInt64(&b.dropped, 1)
		if b.onDropped != nil {
			b.onDropped(item, "unknown_overflow_policy")
		}
		return false
	}
}

// Poll removes and returns the oldest item, false if empty.
func (b *Buffer[T]) Poll() (T, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	var zero T
	if b.size == 0 {
		return zero, false
	}

	item := b.items[b.head]
	b.items[b.head] = zero
	b.head = (b.head + 1) % b.capacity
	b.size--

	return item, true
}

// PollBatch removes and returns up to maxItems
func (b *Buffer[T]) PollBatch(maxItems int) []T {
	if maxItems <= 0 {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}

	itemCount := maxItems
	if itemCount > b.size {
		itemCount = b.size
	}

	result := make([]T, itemCount)
	var zero T

	for i := 0; i < itemCount; i++ {
		result[i] = b.items[b.head]
		b.items[b.head] = zero
		b.head = (b.head + 1) % b.capacity
		b.size--
	}

	return result
}

// Drain removes and returns all items
func (b *Buffer[T]) Drain() []T {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.size == 0 {
		return nil
	}

	result := make([]T, b.size)
	index := 0
	var zero T

	for i := 0; i < b.size; i++ {
		pos := (b.head + i) % b.capacity
		result[index] = b.items[pos]
		b.items[pos] = zero
		index++
	}

	b.head = 0
	b.tail = 0
	b.size = 0

	return result
}

// Peek returns the oldest item without removing it, false if empty
func (b *Buffer[T]) Peek() (T, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var zero T
	if b.size == 0 {
		return zero, false
	}

	return b.items[b.head], true
}

func (b *Buffer[T]) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size
}

func (b *Buffer[T]) Capacity() int {
	return b.capacity
}

func (b *Buffer[T]) Category() DataCategory {
	return b.category
}

func (b *Buffer[T]) Priority() Priority {
	return b.priority
}

func (b *Buffer[T]) IsEmpty() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == 0
}

func (b *Buffer[T]) IsFull() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.size == b.capacity
}

func (b *Buffer[T]) Utilization() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return float64(b.size) / float64(b.capacity)
}

func (b *Buffer[T]) OfferedCount() int64 {
	return atomic.LoadInt64(&b.offered)
}

func (b *Buffer[T]) DroppedCount() int64 {
	return atomic.LoadInt64(&b.dropped)
}

func (b *Buffer[T]) AcceptedCount() int64 {
	return b.OfferedCount() - b.DroppedCount()
}

func (b *Buffer[T]) DropRate() float64 {
	offered := b.OfferedCount()
	if offered == 0 {
		return 0.0
	}
	return float64(b.DroppedCount()) / float64(offered)
}

func (b *Buffer[T]) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()

	var zero T
	for i := 0; i < b.capacity; i++ {
		b.items[i] = zero
	}

	b.head = 0
	b.tail = 0
	b.size = 0
}

func (b *Buffer[T]) GetMetrics() BufferMetrics {
	b.mu.RLock()
	size := b.size
	util := float64(b.size) / float64(b.capacity)
	b.mu.RUnlock()

	return BufferMetrics{
		Category:      b.category,
		Priority:      b.priority,
		Capacity:      b.capacity,
		Size:          size,
		Utilization:   util,
		OfferedCount:  b.OfferedCount(),
		DroppedCount:  b.DroppedCount(),
		AcceptedCount: b.AcceptedCount(),
		DropRate:      b.DropRate(),
		LastUpdated:   time.Now(),
	}
}

type BufferMetrics struct {
	Category      DataCategory `json:"category"`
	Priority      Priority     `json:"priority"`
	Capacity      int          `json:"capacity"`
	Size          int          `json:"size"`
	Utilization   float64      `json:"utilization"`
	OfferedCount  int64        `json:"offered_count"`
	DroppedCount  int64        `json:"dropped_count"`
	AcceptedCount int64        `json:"accepted_count"`
	DropRate      float64      `json:"drop_rate"`
	LastUpdated   time.Time    `json:"last_updated"`
}
