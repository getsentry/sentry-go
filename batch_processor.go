package sentry

import (
	"context"
	"sync"
	"time"
)

const (
	batchSize           = 100
	defaultBatchTimeout = 5 * time.Second
)

type BatchProcessor[T any] struct {
	sendBatch    func([]T)
	itemCh       chan T
	flushCh      chan chan struct{}
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	startOnce    sync.Once
	shutdownOnce sync.Once
	batchTimeout time.Duration
}

func NewBatchProcessor[T any](sendBatch func([]T)) *BatchProcessor[T] {
	return &BatchProcessor[T]{
		itemCh:       make(chan T, batchSize),
		flushCh:      make(chan chan struct{}),
		sendBatch:    sendBatch,
		batchTimeout: defaultBatchTimeout,
	}
}

// WithBatchTimeout sets a custom batch timeout for the processor.
// This is useful for testing or when different timing behavior is needed.
func (p *BatchProcessor[T]) WithBatchTimeout(timeout time.Duration) *BatchProcessor[T] {
	p.batchTimeout = timeout
	return p
}

func (p *BatchProcessor[T]) Send(item T) bool {
	select {
	case p.itemCh <- item:
		return true
	default:
		return false
	}
}

func (p *BatchProcessor[T]) Start() {
	p.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		p.wg.Add(1)
		go p.run(ctx)
	})
}

func (p *BatchProcessor[T]) Flush(timeout <-chan struct{}) {
	done := make(chan struct{})
	select {
	case p.flushCh <- done:
		select {
		case <-done:
		case <-timeout:
		}
	case <-timeout:
	}
}

func (p *BatchProcessor[T]) Shutdown() {
	p.shutdownOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
			p.wg.Wait()
		}
	})
}

func (p *BatchProcessor[T]) run(ctx context.Context) {
	defer p.wg.Done()
	var items []T
	timer := time.NewTimer(0)
	timer.Stop()
	defer timer.Stop()

	for {
		select {
		case item := <-p.itemCh:
			if len(items) == 0 {
				timer.Reset(p.batchTimeout)
			}
			items = append(items, item)
			if len(items) >= batchSize {
				p.sendBatch(items)
				items = nil
			}
		case <-timer.C:
			if len(items) > 0 {
				p.sendBatch(items)
				items = nil
			}
		case done := <-p.flushCh:
		flushDrain:
			for {
				select {
				case item := <-p.itemCh:
					items = append(items, item)
				default:
					break flushDrain
				}
			}

			if len(items) > 0 {
				p.sendBatch(items)
				items = nil
			}
			close(done)
		case <-ctx.Done():
		drain:
			for {
				select {
				case item := <-p.itemCh:
					items = append(items, item)
				default:
					break drain
				}
			}

			if len(items) > 0 {
				p.sendBatch(items)
			}
			return
		}
	}
}
