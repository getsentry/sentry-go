package report

import (
	"sync"
	"testing"

	"github.com/getsentry/sentry-go/protocol"
	"github.com/google/go-cmp/cmp"
)

// Outcomes recorded while reports are taken concurrently all end up in exactly one report.
func TestAggregatorConcurrentRecordAndTake(t *testing.T) {
	t.Parallel()
	const goroutines, perGoroutine = 8, 1000
	aggregator := NewAggregator()
	var taken int64
	take := func() {
		if report := aggregator.TakeReport(); report != nil {
			for _, event := range report.DiscardedEvents {
				taken += event.Quantity
			}
		}
	}

	var recorders sync.WaitGroup
	for range goroutines {
		recorders.Go(func() {
			for range perGoroutine {
				aggregator.Record(ReasonBufferOverflow, protocol.DataCategoryLogItem, 1)
			}
		})
	}
	stop := make(chan struct{})
	var taker sync.WaitGroup
	taker.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				take()
			}
		}
	})
	recorders.Wait()
	close(stop)
	taker.Wait()
	take()

	if diff := cmp.Diff(int64(goroutines*perGoroutine), taken); diff != "" {
		t.Errorf("taken quantity (-want +got):\n%s", diff)
	}
}
