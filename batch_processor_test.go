package sentry

import (
	"sync"
	"testing"
	"time"
)

func TestBatchProcessor_TimerStartsOnFirstItem(t *testing.T) {
	var mu sync.Mutex
	var batches [][]int
	sendBatch := func(items []int) {
		mu.Lock()
		defer mu.Unlock()
		batch := make([]int, len(items))
		copy(batch, items)
		batches = append(batches, batch)
	}

	processor := newBatchProcessor(sendBatch).WithBatchTimeout(50 * time.Millisecond)
	processor.Start()
	defer processor.Shutdown()

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(batches) != 0 {
		mu.Unlock()
		t.Fatalf("expected 0 batches before adding items, got %d", len(batches))
	}
	mu.Unlock()

	processor.Send(42)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(batches) != 1 {
		t.Fatalf("expected 1 batch after timeout, got %d", len(batches))
	}
	if len(batches[0]) != 1 {
		t.Fatalf("expected 1 item in batch, got %d", len(batches[0]))
	}
	if batches[0][0] != 42 {
		t.Errorf("expected item value 42, got %d", batches[0][0])
	}
}
