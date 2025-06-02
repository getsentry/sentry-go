package sentry

import (
	"sync"
	"time"
)

type BufferType string

const (
	InvalidBuffer     BufferType = "invalid"
	ErrorBuffer       BufferType = "error"
	TransactionBuffer BufferType = "transaction"
	LogBuffer         BufferType = "log"
)

type Buffer interface {
	Timeout() time.Duration
	AddItem(event *Event)
	HasBatchSize() bool
	// FlushItems returns all available items by popping them from the buffer without checking the batchsize.
	FlushItems() []*Event
	// FlushItemsIfBatchSize returns all available items by popping them from the buffer and checking that the batchsize is reached.
	FlushItemsIfBatchSize() []*Event
}

func NewBuffer(bufferType BufferType, size int, batchSize int, timeout time.Duration) Buffer {
	var e Buffer
	switch bufferType {
	case TransactionBuffer, ErrorBuffer:
		e = &eventBuffer{
			events:    make([]*Event, 0, size),
			batchSize: batchSize,
			timeout:   timeout,
		}
	case LogBuffer:
		e = &logBuffer{
			events:    make([]*Event, 0, size),
			batchSize: batchSize,
			timeout:   timeout,
		}
	default:
		DebugLogger.Println("Invalid bufferType: fallback to noopBuffer")
		e = &noopBuffer{}
	}

	return e
}

type eventBuffer struct {
	mu        sync.Mutex
	events    []*Event
	timeout   time.Duration
	batchSize int
}

func (b *eventBuffer) Timeout() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.timeout
}

func (b *eventBuffer) AddItem(event *Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= cap(b.events) {
		DebugLogger.Printf("Event with type: %v dropped due to buffer being full", event.Type)
		return
	}
	b.events = append(b.events, event)
}

func (b *eventBuffer) HasBatchSize() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events) >= b.batchSize
}

func (b *eventBuffer) FlushItemsIfBatchSize() []*Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	// handling and popping the buffer items should be handled with one lock to avoid race conditions
	if len(b.events) < b.batchSize {
		return nil
	}

	return b.flushItemsLocked()
}

func (b *eventBuffer) FlushItems() []*Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushItemsLocked()
}

func (b *eventBuffer) flushItemsLocked() []*Event {
	if len(b.events) == 0 {
		return nil
	}
	events := make([]*Event, len(b.events))
	copy(events, b.events)
	b.events = b.events[:0]
	return events
}

type logBuffer struct {
	mu        sync.Mutex
	events    []*Event
	timeout   time.Duration
	batchSize int
}

func (b *logBuffer) Timeout() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.timeout
}

func (b *logBuffer) AddItem(event *Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= cap(b.events) {
		DebugLogger.Printf("Event with type: %v dropped due to buffer being full", event.Type)
		return
	}
	b.events = append(b.events, event)
}

func (b *logBuffer) HasBatchSize() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events) >= b.batchSize
}

func (b *logBuffer) FlushItemsIfBatchSize() []*Event {
	b.mu.Lock()
	defer b.mu.Unlock()

	// handling and popping the buffer items should be handled with one lock to avoid race conditions
	if len(b.events) < b.batchSize {
		return nil
	}

	return b.flushItemsLocked()
}

func (b *logBuffer) FlushItems() []*Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushItemsLocked()
}

func (b *logBuffer) flushItemsLocked() []*Event {
	if len(b.events) == 0 {
		return nil
	}
	logs := make([]Log, len(b.events))
	for i, e := range b.events {
		logs[i] = e.Logs[0]
	}

	event := NewEvent()
	event.Timestamp = time.Now()
	event.EventID = EventID(uuid())
	event.Type = logType
	event.Logs = logs

	b.events = b.events[:0]
	return []*Event{event}
}

func eventToBuffer(eventType string) BufferType {
	switch eventType {
	case "":
		return ErrorBuffer
	case transactionType:
		return TransactionBuffer
	case logType:
		return LogBuffer
	default:
		return InvalidBuffer
	}
}

// noopBuffer is an implementation of Buffer that does nothing.
type noopBuffer struct{}

func (b *noopBuffer) Timeout() time.Duration {
	return time.Second
}

func (b *noopBuffer) AddItem(event *Event) {
}

func (b *noopBuffer) HasBatchSize() bool {
	return false
}

func (b *noopBuffer) FlushItems() []*Event {
	DebugLogger.Println("Buffer incorrectly initialised: no events to flush")
	return nil
}

func (b *noopBuffer) FlushItemsIfBatchSize() []*Event {
	DebugLogger.Println("Buffer incorrectly initialised: no events to flush")
	return nil
}
