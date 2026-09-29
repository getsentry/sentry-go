package telemetry

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
)

// Processor is the top-level object that wraps the scheduler and buffers.
type Processor struct {
	scheduler *Scheduler
}

// NewProcessor creates a new Processor with the given configuration.
func NewProcessor(
	buffers map[ratelimit.Category]Buffer[Item],
	transport transport,
	dsn *protocol.Dsn,
	sdkInfo func() *protocol.SdkInfo,
	recorder report.ClientReportRecorder,
	provider report.ClientReportProvider,
) *Processor {
	scheduler := NewScheduler(buffers, transport, dsn, sdkInfo, recorder, provider)
	scheduler.Start()

	return &Processor{
		scheduler: scheduler,
	}
}

// Add adds a Item to the appropriate buffer based on its category.
//
// The processor should call MakeSerializationSafe to eliminate any race on user mutable fields,
// since the serialization happens on a background goroutine.
func (b *Processor) Add(item Item) bool {
	item.MakeSerializationSafe()
	return b.scheduler.Add(item)
}

// Flush forces all buffers to flush within the given timeout.
func (b *Processor) Flush(timeout time.Duration) bool {
	return b.scheduler.Flush(timeout)
}

// FlushWithContext flushes with a custom context for cancellation.
func (b *Processor) FlushWithContext(ctx context.Context) bool {
	return b.scheduler.FlushWithContext(ctx)
}

// Close stops the buffer, flushes remaining data, and releases resources.
func (b *Processor) Close(timeout time.Duration) {
	b.scheduler.Stop(timeout)
}
