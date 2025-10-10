package telemetry

import (
	"context"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// Scheduler implements a weighted round-robin scheduler for processing buffered events.
type Scheduler struct {
	buffers   map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible]
	transport protocol.TelemetryTransport
	dsn       *protocol.Dsn

	currentCycle []ratelimit.Priority
	cyclePos     int

	ctx          context.Context
	cancel       context.CancelFunc
	processingWg sync.WaitGroup

	mu         sync.Mutex
	cond       *sync.Cond
	startOnce  sync.Once
	finishOnce sync.Once
}

func NewScheduler(
	buffers map[ratelimit.Category]*Buffer[protocol.EnvelopeConvertible],
	transport protocol.TelemetryTransport,
	dsn *protocol.Dsn,
) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	priorityWeights := map[ratelimit.Priority]int{
		ratelimit.PriorityCritical: 5,
		ratelimit.PriorityHigh:     4,
		ratelimit.PriorityMedium:   3,
		ratelimit.PriorityLow:      2,
		ratelimit.PriorityLowest:   1,
	}

	var currentCycle []ratelimit.Priority
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

	s := &Scheduler{
		buffers:      buffers,
		transport:    transport,
		dsn:          dsn,
		currentCycle: currentCycle,
		ctx:          ctx,
		cancel:       cancel,
	}
	s.cond = sync.NewCond(&s.mu)

	return s
}

func (s *Scheduler) Start() {
	s.startOnce.Do(func() {
		s.processingWg.Add(1)
		go s.run()
	})
}

func (s *Scheduler) Stop(timeout time.Duration) {
	s.finishOnce.Do(func() {
		s.Flush(timeout)

		s.cancel()
		s.cond.Broadcast()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.processingWg.Wait()
		}()

		select {
		case <-done:
		case <-time.After(timeout):
			debuglog.Printf("scheduler stop timed out after %v", timeout)
		}
	})
}

func (s *Scheduler) Signal() {
	s.cond.Signal()
}

func (s *Scheduler) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.FlushWithContext(ctx)
}

func (s *Scheduler) FlushWithContext(ctx context.Context) bool {
	s.mu.Lock()
	s.flushBuffers()
	s.mu.Unlock()

	return s.transport.FlushWithContext(ctx)
}

func (s *Scheduler) run() {
	defer s.processingWg.Done()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				s.cond.Broadcast()
			case <-s.ctx.Done():
				return
			}
		}
	}()

	for {
		s.mu.Lock()

		for !s.hasWork() && s.ctx.Err() == nil {
			s.cond.Wait()
		}

		if s.ctx.Err() != nil {
			s.mu.Unlock()
			return
		}

		s.mu.Unlock()
		s.processNextBatch()
	}
}

func (s *Scheduler) hasWork() bool {
	for _, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			return true
		}
	}
	return false
}

func (s *Scheduler) processNextBatch() {
	s.mu.Lock()

	if len(s.currentCycle) == 0 {
		s.mu.Unlock()
		return
	}

	priority := s.currentCycle[s.cyclePos]
	s.cyclePos = (s.cyclePos + 1) % len(s.currentCycle)

	var bufferToProcess *Buffer[protocol.EnvelopeConvertible]
	for category, buffer := range s.buffers {
		if buffer.Priority() == priority && !s.isRateLimited(category) && buffer.IsReadyToFlush() {
			bufferToProcess = buffer
			break
		}
	}

	s.mu.Unlock()

	if bufferToProcess != nil {
		s.processItems(bufferToProcess, false)
	}
}

func (s *Scheduler) processItems(buffer *Buffer[protocol.EnvelopeConvertible], force bool) {
	var items []protocol.EnvelopeConvertible

	if force {
		items = buffer.Drain()
	} else {
		items = buffer.PollIfReady()
	}

	for _, item := range items {
		s.sendItem(item)
	}
}

func (s *Scheduler) sendItem(item protocol.EnvelopeConvertible) {
	envelope, err := item.ToEnvelope(s.dsn)
	if err != nil {
		debuglog.Printf("error converting item to envelope: %v", err)
		return
	}
	if err := s.transport.SendEnvelope(envelope); err != nil {
		debuglog.Printf("error sending envelope: %v", err)
	}
}

func (s *Scheduler) flushBuffers() {
	for _, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			s.processItems(buffer, true)
		}
	}
}

func (s *Scheduler) isRateLimited(category ratelimit.Category) bool {
	return s.transport.IsRateLimited(category)
}
