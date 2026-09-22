package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/util"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
)

// Scheduler implements a weighted round-robin scheduler for processing buffered events.
type Scheduler struct {
	buffers   map[ratelimit.Category]Buffer[Item]
	transport Transport
	dsn       *protocol.Dsn
	sdkInfo   func() *protocol.SdkInfo
	recorder  report.ClientReportRecorder

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

// queueFullBackoff delays scheduling after a transport reports a full queue.
const queueFullBackoff = 100 * time.Millisecond

func NewScheduler(
	buffers map[ratelimit.Category]Buffer[Item],
	transport Transport,
	dsn *protocol.Dsn,
	sdkInfo func() *protocol.SdkInfo,
	recorder report.ClientReportRecorder,
) *Scheduler {
	if recorder == nil {
		recorder = report.NoopRecorder()
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel is stored in s.cancel and called in Shutdown()

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
		sdkInfo:      sdkInfo,
		recorder:     recorder,
		currentCycle: currentCycle,
		ctx:          ctx,
		cancel:       cancel,
	}
	s.cond = sync.NewCond(&s.mu)

	return s
}

func (s *Scheduler) resolveSdkInfo() *protocol.SdkInfo {
	if s.sdkInfo == nil {
		return &protocol.SdkInfo{}
	}
	return s.sdkInfo()
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

func (s *Scheduler) Add(item Item) bool {
	category := item.GetCategory()
	buffer, exists := s.buffers[category]
	if !exists {
		return false
	}

	accepted := buffer.Offer(item)
	if accepted {
		s.Signal()
	}
	return accepted
}

func (s *Scheduler) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.FlushWithContext(ctx)
}

func (s *Scheduler) FlushWithContext(ctx context.Context) bool {
	s.flushBuffers()
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
		if s.processNextBatch() {
			// Keep items in the prioritized buffers while the transport is full.
			select {
			case <-time.After(queueFullBackoff):
			case <-s.ctx.Done():
				return
			}
		}
	}
}

func (s *Scheduler) hasWork() bool {
	for _, buffer := range s.buffers {
		if buffer.IsReadyToFlush() {
			return true
		}
	}
	return false
}

// processNextBatch reports whether the transport queue was full.
func (s *Scheduler) processNextBatch() bool {
	if len(s.currentCycle) == 0 {
		return false
	}

	priority := s.currentCycle[s.cyclePos]
	s.cyclePos = (s.cyclePos + 1) % len(s.currentCycle)

	var bufferToProcess Buffer[Item]
	var categoryToProcess ratelimit.Category
	for category, buffer := range s.buffers {
		if buffer.Priority() == priority && buffer.IsReadyToFlush() {
			bufferToProcess = buffer
			categoryToProcess = category
			break
		}
	}

	if bufferToProcess != nil {
		return s.processItems(bufferToProcess, categoryToProcess, false)
	}
	return false
}

func (s *Scheduler) processItems(buffer Buffer[Item], category ratelimit.Category, force bool) (full bool) {
	var items []Item

	if force {
		items = buffer.Drain()
	} else {
		items = buffer.PollIfReady()
	}

	if len(items) == 0 {
		return false
	}

	for _, item := range s.envelopeConvertibles(category, items) {
		if errors.Is(s.sendItem(item), ErrQueueFull) {
			full = true
		}
	}
	return full
}

// envelopeConvertibles converts single items or batches to satisfy the EnvelopeConvertible interface.
func (s *Scheduler) envelopeConvertibles(category ratelimit.Category, items []Item) []EnvelopeConvertible {
	switch category {
	case ratelimit.CategoryLog, ratelimit.CategoryTraceMetric:
		return []EnvelopeConvertible{NewItemContainer(category, items)}
	default:
		convertibles := make([]EnvelopeConvertible, 0, len(items))
		for _, item := range items {
			if convertible, ok := item.(EnvelopeConvertible); ok {
				convertibles = append(convertibles, convertible)
				continue
			}
			debuglog.Printf("item does not implement envelope conversion: %T", item)
		}
		return convertibles
	}
}

func (s *Scheduler) sendItem(item EnvelopeConvertible) error {
	header := &protocol.EnvelopeHeader{
		EventID: item.GetEventID(),
		SentAt:  time.Now(),
		Dsn:     s.dsn,
		Trace:   item.GetDynamicSamplingContext(),
		Sdk:     item.GetSdkInfo(),
	}
	if header.EventID == "" {
		header.EventID = util.GenerateEventID()
	}
	if header.Sdk == nil {
		header.Sdk = s.resolveSdkInfo()
	}

	envelope, err := item.ToEnvelope(header)
	if err != nil {
		debuglog.Printf("error while converting to envelope: %v", err)
		s.recorder.RecordItem(report.ReasonInternalError, item)
		return err
	}
	if err := s.transport.SendEnvelope(envelope); err != nil {
		debuglog.Printf("error sending envelope: %v", err)
		reason := report.ReasonSendError
		if errors.Is(err, ErrQueueFull) {
			reason = report.ReasonQueueOverflow
		}
		s.recorder.RecordForEnvelope(reason, envelope)
		return err
	}
	return nil
}

func (s *Scheduler) flushBuffers() {
	for category, buffer := range s.buffers {
		if !buffer.IsEmpty() {
			s.processItems(buffer, category, true)
		}
	}
}
