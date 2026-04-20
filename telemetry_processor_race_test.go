package sentry

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/report"
)

// This race is between json.Marshal on the scheduler goroutine and user
// mutations on the calling goroutine. It validates that we don't hold
// mutable user data on the SDK.
func TestTelemetryProcessorRace(_ *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdkInfo := &protocol.SdkInfo{Name: "test-sdk", Version: "1.0.0"}

	buffers := map[ratelimit.Category]telemetry.Buffer[protocol.TelemetryItem]{
		ratelimit.CategoryError: telemetry.NewRingBuffer[protocol.TelemetryItem](
			ratelimit.CategoryError, 100, telemetry.OverflowPolicyDropOldest, 1, 0, report.NoopRecorder(),
		),
	}

	proc := telemetry.NewProcessor(buffers, transport, dsn, func() *protocol.SdkInfo { return sdkInfo }, report.NoopRecorder())
	defer proc.Close(2 * time.Second)

	const numEvents = 100
	const numMutations = 500

	var wg sync.WaitGroup

	for i := 0; i < numEvents; i++ {
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

		proc.Add(event)
		// Yield to let the scheduler goroutine start processing
		runtime.Gosched()

		wg.Add(1)
		go func(ev *Event, idx int) {
			defer wg.Done()
			for j := 0; j < numMutations; j++ {
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
	proc.Flush(2 * time.Second)
}
