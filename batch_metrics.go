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

func (l *BatchMeter) Start() {
	l.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		l.cancel = cancel
		l.wg.Add(1)
		go l.run(ctx)
	})
}

func (l *BatchMeter) Flush(timeout <-chan struct{}) {
	done := make(chan struct{})
	select {
	case l.flushCh <- done:
		select {
		case <-done:
		case <-timeout:
		}
	case <-timeout:
	}
}

func (l *BatchMeter) Shutdown() {
	l.shutdownOnce.Do(func() {
		if l.cancel != nil {
			l.cancel()
			l.wg.Wait()
		}
	})
}

func (l *BatchMeter) run(ctx context.Context) {
	defer l.wg.Done()
	var metrics []Metric
	timer := time.NewTimer(batchTimeout)
	defer timer.Stop()

	for {
		select {
		case metric := <-l.metricsCh:
			metrics = append(metrics, metric)
			if len(metrics) >= batchSize {
				l.processEvent(metrics)
				metrics = nil
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(batchTimeout)
			}
		case <-timer.C:
			if len(metrics) > 0 {
				l.processEvent(metrics)
				metrics = nil
			}
			timer.Reset(batchTimeout)
		case done := <-l.flushCh:
		flushDrain:
			for {
				select {
				case metric := <-l.metricsCh:
					metrics = append(metrics, metric)
				default:
					break flushDrain
				}
			}

			if len(metrics) > 0 {
				l.processEvent(metrics)
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
				case metric := <-l.metricsCh:
					metrics = append(metrics, metric)
				default:
					break drain
				}
			}

			if len(metrics) > 0 {
				l.processEvent(metrics)
			}
			return
		}
	}
}

func (l *BatchMeter) processEvent(metrics []Metric) {
	event := NewEvent()
	event.Timestamp = time.Now()
	event.EventID = EventID(uuid())
	event.Type = logEvent.Type
	event.Metrics = metrics
	l.client.Transport.SendEvent(event)
}
