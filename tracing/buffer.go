package sentrytracing

import (
	"container/list"
	"sync"
)

// TraceBuffer implements a FIFO queue that groups spans by trace ID
type TraceBuffer struct {
	mu sync.Mutex
	// Map from trace ID to list of spans with that trace ID
	traces map[string]*list.List
	// Overall FIFO order of trace IDs
	order *list.List
}

// NewTraceBuffer creates a new TraceBuffer
func NewTraceBuffer() *TraceBuffer {
	return &TraceBuffer{
		traces: make(map[string]*list.List),
		order:  list.New(),
	}
}

// Add adds a span to the buffer, maintaining FIFO order within each trace ID bucket
func (b *TraceBuffer) Add(span *Span) {
	b.mu.Lock()
	defer b.mu.Unlock()

	traceID := span.TraceID.Hex()

	// Get or create list for this trace ID
	spans, exists := b.traces[traceID]
	if !exists {
		spans = list.New()
		b.traces[traceID] = spans
		// Add trace ID to order list since this is first span for this trace
		b.order.PushBack(traceID)
	}

	// Add span to end of list for this trace
	spans.PushBack(span)
}

// Remove removes and returns the oldest span from the oldest trace ID bucket
func (b *TraceBuffer) Remove() *Span {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Get oldest trace ID
	if b.order.Len() == 0 {
		return nil
	}
	traceID := b.order.Front().Value.(string)

	// Get spans for this trace
	spans := b.traces[traceID]
	if spans.Len() == 0 {
		// No spans left for this trace, remove trace ID
		delete(b.traces, traceID)
		b.order.Remove(b.order.Front())
		return nil
	}

	// Remove oldest span
	span := spans.Remove(spans.Front()).(*Span)

	// If no more spans for this trace, remove trace ID
	if spans.Len() == 0 {
		delete(b.traces, traceID)
		b.order.Remove(b.order.Front())
	}

	return span
}

// Len returns the total number of spans in the buffer
func (b *TraceBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	total := 0
	for _, spans := range b.traces {
		total += spans.Len()
	}
	return total
}
