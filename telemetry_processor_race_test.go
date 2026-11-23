package sentry

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
)

// This race is between json.Marshal on the scheduler goroutine and user
// mutations on the calling goroutine. It validates that we don't hold
// mutable user data on the SDK.
func TestTelemetryProcessorRace(_ *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	buffers := map[ratelimit.Category]telemetry.Buffer[telemetry.Item]{
		ratelimit.CategoryError: telemetry.NewRingBuffer[telemetry.Item](
			ratelimit.CategoryError, 100, telemetry.OverflowPolicyDropOldest, 1, 0, nil,
		),
	}

	proc := telemetry.NewProcessor(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil)
	defer proc.Close(2 * time.Second)

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

		proc.Add(context.Background(), event)
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

				// Slice shrink/grow — "index out of range"
				ev.Breadcrumbs = append(ev.Breadcrumbs, &Breadcrumb{
					Message:   fmt.Sprintf("breadcrumb-%d-%d", idx, j),
					Timestamp: time.Now(),
				})
				if j%10 == 0 && len(ev.Breadcrumbs) > 1 {
					ev.Breadcrumbs = ev.Breadcrumbs[:1]
				}
			}
		}(event, i)
	}

	wg.Wait()
	testutils.FlushWithTimeout(proc, 2*time.Second)
}

func TestTelemetryProcessorRaceSpans(_ *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	buffers := map[ratelimit.Category]telemetry.Buffer[telemetry.Item]{
		ratelimit.CategoryTransaction: telemetry.NewRingBuffer[telemetry.Item](
			ratelimit.CategoryTransaction, 100, telemetry.OverflowPolicyDropOldest, 1, 0, nil,
		),
	}

	proc := telemetry.NewProcessor(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, nil)
	defer proc.Close(2 * time.Second)

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

		event := NewEvent()
		event.EventID = EventID(fmt.Sprintf("%032x", i))
		event.Type = transactionType
		event.Transaction = fmt.Sprintf("tx %d", i)
		event.StartTime = time.Now()
		event.Timestamp = time.Now()
		event.Spans = children

		proc.Add(context.Background(), event)
		runtime.Gosched()

		wg.Add(1)
		go func(spans []*Span, idx int) {
			defer wg.Done()
			for j := range numMutations {
				for _, sp := range spans {
					sp.SetTag(fmt.Sprintf("tag-%d-%d", idx, j), fmt.Sprintf("v-%d", j))
					sp.SetData(fmt.Sprintf("data-%d-%d", idx, j), j)
				}
			}
		}(children, i)
	}

	wg.Wait()
	testutils.FlushWithTimeout(proc, 2*time.Second)
}
