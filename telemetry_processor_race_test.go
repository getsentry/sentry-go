package sentry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// This race is between json.Marshal on the scheduler goroutine and user
// mutations on the calling goroutine. It validates that we don't hold
// mutable user data on the SDK.
func TestTelemetryProcessorRace(_ *testing.T) {
	// MockTransport would encode on the calling goroutine.
	client, _ := NewClient(ClientOptions{Dsn: testDsn, Transport: &NoopTransport{}})

	const numEvents = 100
	const numMutations = 500

	var wg sync.WaitGroup

	for i := range numEvents {
		event := NewEvent()
		event.EventID = EventID(fmt.Sprintf("%032x", i))
		event.Level = LevelError
		event.Message = fmt.Sprintf("test event %d", i)
		event.Contexts = map[string]Context{
			"initial_context": {"key": "value"},
			"race_context":    {"initial_key": "initial_value"},
		}
		event.Breadcrumbs = []*Breadcrumb{
			{Message: "initial breadcrumb", Timestamp: time.Now()},
		}

		client.CaptureEvent(context.Background(), event)
		// Yield to let the scheduler goroutine start processing
		runtime.Gosched()

		wg.Add(1)
		go func(ev *Event, idx int) {
			defer wg.Done()
			for j := range numMutations {
				// Map writes — "concurrent map read and map write"
				ev.Contexts[fmt.Sprintf("ctx-%d-%d", idx, j)] = Context{
					"dynamic": fmt.Sprintf("value-%d", j),
				}

				ev.Breadcrumbs[0].Message = fmt.Sprintf("breadcrumb-%d-%d", idx, j)
			}
		}(event, i)
	}

	// Close before waiting, so that encoding is not ordered after the mutations.
	client.Close()
	wg.Wait()
}

func TestTelemetryProcessorRaceSpans(_ *testing.T) {
	// MockTransport would encode on the calling goroutine.
	client, _ := NewClient(ClientOptions{Dsn: testDsn, Transport: &NoopTransport{}})

	const numTransactions = 100
	const numChildren = 4
	const numMutations = 500

	var wg sync.WaitGroup

	for i := range numTransactions {
		children := make([]*Span, 0, numChildren)
		for c := range numChildren {
			child := &Span{
				TraceID:   TraceID{},
				SpanID:    SpanID{},
				Op:        fmt.Sprintf("child.op.%d", c),
				StartTime: time.Now(),
				EndTime:   time.Now(),
				Tags:      map[string]string{"initial": "value"},
				Data:      map[string]any{"initial": "value"},
			}
			children = append(children, child)
		}

		tx := &Transaction{
			EventID:   EventID(fmt.Sprintf("%032x", i)),
			Name:      fmt.Sprintf("tx %d", i),
			StartTime: time.Now(),
			EndTime:   time.Now(),
			Spans:     children,
			Tags:      map[string]string{"initial": "value"},
			Contexts:  map[string]Context{"initial": {"key": "value"}},
			Request:   &Request{Headers: map[string]string{"initial": "value"}},
			Trace:     TraceContext{Data: map[string]any{"initial": "value"}},
		}

		client.captureTransaction(context.Background(), tx, nil)
		runtime.Gosched()

		wg.Add(1)
		go func(spans []*Span, idx int) {
			defer wg.Done()
			for j := range numMutations {
				for _, sp := range spans {
					sp.SetTag(fmt.Sprintf("tag-%d-%d", idx, j), fmt.Sprintf("v-%d", j))
					sp.SetData(fmt.Sprintf("data-%d-%d", idx, j), j)
				}
				key := fmt.Sprintf("key-%d", j)
				tx.Tags[key] = "value"
				tx.Contexts[key] = Context{}
				tx.Request.Headers[key] = "value"
				tx.Trace.Data[key] = j
			}
		}(children, i)
	}

	// Close before waiting, so that encoding is not ordered after the mutations.
	client.Close()
	wg.Wait()
}
