package sentry

import (
	"context"
	"sync"
	"time"
)

type BatchMeter struct {
	client       *Client
	metricsCh    chan Metric
	flushCh      chan chan struct{}
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	startOnce    sync.Once
	shutdownOnce sync.Once
}

func NewBatchMeter(client *Client) *BatchMeter {
	return &BatchMeter{
		client:    client,
		metricsCh: make(chan Metric, batchSize),
		flushCh:   make(chan chan struct{}),
	}
}

func (m *BatchMeter) Start() {
	m.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancel = cancel
		m.wg.Add(1)
		go m.run(ctx)
	})
}

func (m *BatchMeter) Flush(timeout <-chan struct{}) {
	done := make(chan struct{})
	select {
	case m.flushCh <- done:
		select {
		case <-done:
		case <-timeout:
		}
	case <-timeout:
	}
}

func (m *BatchMeter) Shutdown() {
	m.shutdownOnce.Do(func() {
		if m.cancel != nil {
			m.cancel()
			m.wg.Wait()
		}
	})
}

func (m *BatchMeter) run(ctx context.Context) {
	defer m.wg.Done()
	var metrics []Metric
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()

	for {
		select {
		case metric := <-m.metricsCh:
			metrics = append(metrics, metric)
			if len(metrics) >= batchSize {
				m.processEvent(metrics)
				metrics = nil
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(batchTimeout)
			}
		case <-timer.C:
			if len(metrics) > 0 {
				m.processEvent(metrics)
				metrics = nil
			}
			timer.Reset(batchTimeout)
		case done := <-m.flushCh:
		flushDrain:
			for {
				select {
				case metric := <-m.metricsCh:
					metrics = append(metrics, metric)
				default:
					break flushDrain
				}
			}

			if len(metrics) > 0 {
				m.processEvent(metrics)
				metrics = nil
			}
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(batchTimeout)
			close(done)
		case <-ctx.Done():
		drain:
			for {
				select {
				case metric := <-m.metricsCh:
					metrics = append(metrics, metric)
				default:
					break drain
				}
			}

			if len(metrics) > 0 {
				m.processEvent(metrics)
			}
			return
		}
	}
}

func (m *BatchMeter) processEvent(metrics []Metric) {
	event := NewEvent()
	event.Timestamp = time.Now()
	event.EventID = EventID(uuid())
	event.Type = traceMetricEvent.Type
	event.Metrics = metrics
	m.client.Transport.SendEvent(event)
}
