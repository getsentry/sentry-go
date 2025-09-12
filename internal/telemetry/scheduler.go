package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/http"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// EnvelopeScheduler implements a simplified weighted round-robin scheduler for processing buffered events.
// It polls buffers based on priority weights and flushes entire buffers when selected.
type EnvelopeScheduler struct {
	buffers   map[DataCategory]*Buffer[EnvelopeConvertible]
	config    *BufferConfig
	dsn       *sentry.Dsn
	transport http.Transport

	currentCycle []Priority
	cyclePos     int

	ctx          context.Context
	cancel       context.CancelFunc
	processingWg sync.WaitGroup

	mu         sync.RWMutex
	startOnce  sync.Once
	finishOnce sync.Once
}

func NewEnvelopeScheduler(
	ctx context.Context,
	buffers map[DataCategory]*Buffer[EnvelopeConvertible],
	transport http.Transport,
	dsn *sentry.Dsn,
	config *BufferConfig,
) *EnvelopeScheduler {
	schedulerCtx, cancel := context.WithCancel(ctx)

	var currentCycle []Priority
	for priority, weight := range config.PriorityWeights {
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

	return &EnvelopeScheduler{
		buffers:      buffers,
		transport:    transport,
		dsn:          dsn,
		config:       config,
		currentCycle: currentCycle,
		ctx:          schedulerCtx,
		cancel:       cancel,
	}
}

func (s *EnvelopeScheduler) Start() {
	s.startOnce.Do(func() {
		s.processingWg.Add(1)
		go s.run()
	})
}

func (s *EnvelopeScheduler) Stop(timeout time.Duration) {
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
			sentry.DebugLogger.Println("could not flush all buffers: timeout reached")
		}
	})
}

func (s *EnvelopeScheduler) Flush() {
	s.flushAllBuffers()
}

func (s *EnvelopeScheduler) run() {
	defer s.processingWg.Done()

	ticker := time.NewTicker(10 * time.Millisecond) // 100Hz processing
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.processNextPriority()
		}
	}
}

func (s *EnvelopeScheduler) processNextPriority() {
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

		if s.transport.IsRateLimited(ratelimit.Category(category)) {
			return
		}

		if buffer.IsEmpty() {
			continue
		}

		s.flushBuffer(category, buffer)
	}
}

func (s *EnvelopeScheduler) flushBuffer(category DataCategory, buffer *Buffer[EnvelopeConvertible]) {
	items := buffer.Drain()
	if len(items) == 0 {
		return
	}

	if s.transport.IsRateLimited(ratelimit.Category(category)) {
		return
	}

	batchSize := s.config.BatchSizes[category]
	if batchSize <= 0 {
		batchSize = 1
	}

	batches := s.batchBySize(items, batchSize)
	sentAt := time.Now()
	for _, batch := range batches {
		s.sendBatch(batch, sentAt)
	}
}

func (s *EnvelopeScheduler) flushAllBuffers() {
	for category, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			s.flushBuffer(category, buffer)
		}
	}
}

func (s *EnvelopeScheduler) batchBySize(items []EnvelopeConvertible, maxBatchSize int) [][]EnvelopeConvertible {
	var batches [][]EnvelopeConvertible

	for i := 0; i < len(items); i += maxBatchSize {
		end := i + maxBatchSize
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}

	return batches
}

func (s *EnvelopeScheduler) sendBatch(items []EnvelopeConvertible, sentAt time.Time) {
	if len(items) == 0 {
		return
	}

	combinedItems := s.combineItems(items)
	for _, item := range combinedItems {
		envelope, err := item.ToEnvelope(s.dsn, sentAt)
		if err != nil {
			sentry.DebugLogger.Printf("error converting to envelope: %e", err)
			continue
		}

		if envelope != nil {
			err = s.transport.SendEnvelope(envelope)
			if err != nil {
				// TODO: Log error and potentially retry
			}
		}
	}
}

func (s *EnvelopeScheduler) combineItems(items []EnvelopeConvertible) []EnvelopeConvertible {
	if len(items) <= 1 {
		return items
	}

	var result []EnvelopeConvertible
	current := items[0]

	for i := 1; i < len(items); i++ {
		if current.CanBatchWith(items[i]) {
			current = current.BatchWith(items[i])
		} else {
			result = append(result, current)
			current = items[i]
		}
	}
	result = append(result, current)

	return result
}
