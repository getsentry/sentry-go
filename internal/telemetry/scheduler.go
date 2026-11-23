package telemetry

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
)

const clientReportInterval = 30 * time.Second

// Scheduler implements a weighted round-robin scheduler for processing buffered events.
type Scheduler struct {
	buffers   map[ratelimit.Category]Buffer[Item]
	transport Transport
	dsn       *protocol.Dsn
	sdkInfo   func() *protocol.SdkInfo
	reports   *report.Aggregator

	currentCycle []Priority
	cyclePos     int

	ctx          context.Context
	cancel       context.CancelFunc
	processingWg sync.WaitGroup

	// processing serializes buffer-to-transport handoffs and allows cancelable waits.
	processing chan struct{}

	mu         sync.Mutex
	cond       *sync.Cond
	closed     bool // guarded by mu
	startOnce  sync.Once
	finishOnce sync.Once
}

func NewScheduler(
	buffers map[ratelimit.Category]Buffer[Item],
	transport Transport,
	dsn *protocol.Dsn,
	sdkInfo func() *protocol.SdkInfo,
	reports *report.Aggregator,
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
			for range weight {
				currentCycle = append(currentCycle, priority)
			}
		}
	}

	s := &Scheduler{
		buffers:      buffers,
		transport:    transport,
		dsn:          dsn,
		sdkInfo:      sdkInfo,
		reports:      reports,
		currentCycle: currentCycle,
		ctx:          ctx,
		cancel:       cancel,
		processing:   make(chan struct{}, 1),
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
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		// Reject new items
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		s.FlushWithContext(ctx)

		// Cancel under s.mu
		s.mu.Lock()
		s.cancel()
		s.cond.Broadcast()
		s.mu.Unlock()

		done := make(chan struct{})
		go func() {
			defer close(done)
			s.processingWg.Wait()
		}()

		select {
		case <-done:
		case <-ctx.Done():
			debuglog.Printf("scheduler stop timed out after %v", timeout)
		}
	})
}

func (s *Scheduler) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}

	accepted := buffer.Offer(item)
	if accepted {
		s.Signal()
	}
	return accepted
}

func (s *Scheduler) FlushWithContext(ctx context.Context) bool {
	select {
	case s.processing <- struct{}{}:
		defer func() { <-s.processing }()
	case <-ctx.Done():
		return false
	}

	if !s.flushBuffers(ctx) {
		return false
	}
	if !s.transport.FlushWithContext(ctx) {
		return false
	}
	if s.sendClientReport(ctx) {
		return s.transport.FlushWithContext(ctx)
	}
	return true
}

func (s *Scheduler) run() {
	defer s.processingWg.Done()

	s.processingWg.Go(func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		reportTicker := time.NewTicker(clientReportInterval)
		defer ticker.Stop()
		defer reportTicker.Stop()

		for {
			select {
			case <-ticker.C:
				s.cond.Broadcast()
			case <-reportTicker.C:
				select {
				case s.processing <- struct{}{}:
					s.sendClientReport(s.ctx)
					<-s.processing
				case <-s.ctx.Done():
					return
				}
			case <-s.ctx.Done():
				return
			}
		}
	})

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
		if buffer.IsReadyToFlush() {
			return true
		}
	}
	return false
}

func (s *Scheduler) processNextBatch() {
	select {
	case s.processing <- struct{}{}:
		defer func() { <-s.processing }()
	case <-s.ctx.Done():
		return
	}

	if len(s.currentCycle) == 0 {
		return
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
		s.processItems(s.ctx, bufferToProcess, categoryToProcess, false)
	}
}

func (s *Scheduler) processItems(ctx context.Context, buffer Buffer[Item], category ratelimit.Category, force bool) {
	var items []Item

	if force {
		items = buffer.Drain()
	} else {
		items = buffer.PollIfReady()
	}

	if len(items) == 0 {
		return
	}

	for _, item := range s.envelopeConvertibles(category, items) {
		s.sendItem(ctx, item, true)
	}
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

// sendItem converts the item to an envelope and attaches pending client reports.
func (s *Scheduler) sendItem(ctx context.Context, item EnvelopeConvertible, wait bool) bool {
	header := &protocol.EnvelopeHeader{
		EventID: item.GetEventID(),
		SentAt:  time.Now(),
		Dsn:     s.dsn,
		Trace:   item.GetDynamicSamplingContext(),
		Sdk:     item.GetSdkInfo(),
	}
	if header.Sdk == nil {
		header.Sdk = s.resolveSdkInfo()
	}

	envelope, err := item.ToEnvelope(header)
	if err != nil {
		debuglog.Printf("error while converting to envelope: %v", err)
		item.DiscardCounts().Record(s.reports, report.ReasonInternalError)
		return false
	}
	discard := Discard{Counts: item.DiscardCounts(), Report: s.attachReport(envelope)}
	return s.sendEnvelope(WithDiscard(ctx, discard), envelope, wait)
}

// sendEnvelope hands envelope to the transport. If the transport rejects it, the Discard carried by ctx is recorded.
//
// With wait, a queue-full rejection flushes the transport and retries once.
func (s *Scheduler) sendEnvelope(ctx context.Context, envelope *protocol.Envelope, wait bool) bool {
	err := s.transport.SendEnvelope(ctx, envelope)
	if wait && errors.Is(err, ErrQueueFull) && s.transport.FlushWithContext(ctx) {
		err = s.transport.SendEnvelope(ctx, envelope)
	}
	if err == nil {
		return true
	}

	debuglog.Printf("error sending envelope: %v", err)
	reason := report.ReasonSendError
	if errors.Is(err, ErrQueueFull) {
		reason = report.ReasonQueueOverflow
	}
	DiscardFrom(ctx).Record(s.reports, reason)
	return false
}

func (s *Scheduler) flushBuffers(ctx context.Context) bool {
	for category, buffer := range s.buffers {
		if ctx.Err() != nil {
			return false
		}
		if !buffer.IsEmpty() {
			s.processItems(ctx, buffer, category, true)
		}
	}
	return ctx.Err() == nil
}

// sendClientReport submits a standalone client report, if one is pending.
func (s *Scheduler) sendClientReport(ctx context.Context) bool {
	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{
		SentAt: time.Now(),
		Dsn:    s.dsn,
		Sdk:    s.resolveSdkInfo(),
	})
	discard := Discard{Report: s.attachReport(envelope)}
	if len(envelope.Items) == 0 {
		return false
	}
	return s.sendEnvelope(WithDiscard(ctx, discard), envelope, false)
}

// attachReport adds the pending client report to envelope and returns it.
func (s *Scheduler) attachReport(envelope *protocol.Envelope) *report.ClientReport {
	r := s.reports.TakeReport()
	if r == nil {
		return nil
	}
	item, err := r.ToEnvelopeItem()
	if err != nil {
		debuglog.Printf("failed to serialize client report: %v", err)
		return nil
	}
	envelope.AddItem(item)
	return r
}
