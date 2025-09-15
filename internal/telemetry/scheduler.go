package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// DebugLogger interface allows the scheduler to log debug messages
// This avoids importing the main sentry package
type DebugLogger interface {
	Printf(format string, v ...interface{})
}

// defaultDebugLogger is a no-op logger
type defaultDebugLogger struct{}

func (d defaultDebugLogger) Printf(format string, v ...interface{}) {
	// No-op by default, can be set by client
}

var debugLogger DebugLogger = defaultDebugLogger{}

// SetDebugLogger sets the debug logger for the scheduler
func SetDebugLogger(logger DebugLogger) {
	debugLogger = logger
}

// Scheduler implements a weighted round-robin scheduler for processing buffered events.
// It works with any type that implements protocol.EnvelopeConvertible.
type Scheduler struct {
	buffers   map[DataCategory]*Buffer[protocol.EnvelopeConvertible]
	transport protocol.TelemetryTransport
	dsn       *protocol.Dsn

	currentCycle []Priority
	cyclePos     int

	ctx          context.Context
	cancel       context.CancelFunc
	processingWg sync.WaitGroup

	mu         sync.RWMutex
	startOnce  sync.Once
	finishOnce sync.Once
}

// NewScheduler creates a new telemetry scheduler.
func NewScheduler(
	buffers map[DataCategory]*Buffer[protocol.EnvelopeConvertible],
	transport protocol.TelemetryTransport,
	dsn *protocol.Dsn,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	priorityWeights := map[Priority]int{
		PriorityCritical: 5,
		PriorityHigh:     4,
		PriorityMedium:   3,
		PriorityLow:      2,
		PriorityLowest:   1,
	}

	var currentCycle []Priority
	for priority, weight := range priorityWeights {
		hasBuffers := false
		for _, buffer := range buffers {
			if buffer.Priority() == priority {
				hasBuffers = true
				break
			}
		}

		if hasBuffers {
			for i := 0; i < weight; i++ {
				currentCycle = append(currentCycle, priority)
			}
		}
	}

	return &Scheduler{
		buffers:      buffers,
		transport:    transport,
		dsn:          dsn,
		currentCycle: currentCycle,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start begins the telemetry processing loop
func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		s.processingWg.Add(1)
		go s.run()
	})
}

// Stop gracefully shuts down the scheduler
func (s *Scheduler) Stop(timeout time.Duration) {
	s.finishOnce.Do(func() {
		s.cancel()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.processingWg.Wait()
			s.flushAllBuffers()
		}()

		select {
		case <-done:
		case <-time.After(timeout):
		}
	})
}

// Flush processes all items in all buffers immediately
func (s *Scheduler) Flush() {
	s.flushAllBuffers()
}

// run is the main processing loop
func (s *Scheduler) run() {
	defer s.processingWg.Done()

	ticker := time.NewTicker(100 * time.Millisecond) // 10Hz processing
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processNextEnvelope()
		}
	}
}

// processNextEnvelope processes one envelope according to the weighted round-robin schedule
func (s *Scheduler) processNextEnvelope() {
	s.mu.Lock()

	if len(s.currentCycle) == 0 {
		s.mu.Unlock()
		return
	}

	priority := s.currentCycle[s.cyclePos]
	s.cyclePos = (s.cyclePos + 1) % len(s.currentCycle)

	s.mu.Unlock()

	for category, buffer := range s.buffers {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		if buffer.Priority() != priority {
			continue
		}

		if s.isRateLimited(category) {
			continue
		}

		if buffer.IsEmpty() {
			continue
		}

		s.processOneItem(category, buffer)
	}
}

// processOneItem processes a single item from the buffer
func (s *Scheduler) processOneItem(category DataCategory, buffer *Buffer[protocol.EnvelopeConvertible]) {
	if item, ok := buffer.Poll(); ok {
		if !s.isRateLimited(category) {
			s.sendItem(item)
		}
	}
}

// sendItem converts an item to an envelope and sends it via transport
func (s *Scheduler) sendItem(item protocol.EnvelopeConvertible) {
	envelope, err := item.ToEnvelope(s.dsn)
	if err != nil {
		debugLogger.Printf("error converting item to envelope: %v", err)
		return
	}
	if err := s.transport.SendEnvelope(envelope); err != nil {
		debugLogger.Printf("error sending envelope: %v", err)
	}
}

// flushAllBuffers drains all buffers and sends their contents
func (s *Scheduler) flushAllBuffers() {
	for category, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			s.flushBuffer(category, buffer)
		}
	}
}

// flushBuffer drains a specific buffer and sends all its contents
func (s *Scheduler) flushBuffer(category DataCategory, buffer *Buffer[protocol.EnvelopeConvertible]) {
	items := buffer.Drain()
	if len(items) == 0 {
		return
	}

	for _, item := range items {
		if !s.isRateLimited(category) {
			s.sendItem(item)
		}
	}
}

// isRateLimited checks if the given category is currently rate limited
func (s *Scheduler) isRateLimited(category DataCategory) bool {
	return s.transport.IsRateLimited(ratelimit.Category(string(category)))
}
