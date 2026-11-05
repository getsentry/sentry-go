package telemetry

import (
	"context"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/report"
)

// DiscardCounts holds client report counts for a dropped item.
type DiscardCounts struct {
	Category ratelimit.Category
	Quantity int64 // of Category
	Spans    int64 // including the transaction itself
	Bytes    int64 // log_byte for logs, trace_metric_byte for metrics, attachment bytes for events
}

// Add returns c with the counts of other added, keeping c's Category.
func (c DiscardCounts) Add(other DiscardCounts) DiscardCounts {
	c.Quantity += other.Quantity
	c.Spans += other.Spans
	c.Bytes += other.Bytes
	return c
}

// Record records c as discarded for reason.
func (c DiscardCounts) Record(reports *report.Aggregator, reason report.DiscardReason) {
	reports.Record(reason, c.Category, c.Quantity)
	reports.Record(reason, ratelimit.CategorySpan, c.Spans)
	reports.Record(reason, byteCategory(c.Category), c.Bytes)
}

// byteCategory returns the category that counts the bytes of an item in category c.
func byteCategory(c ratelimit.Category) ratelimit.Category {
	switch c {
	case ratelimit.CategoryLog:
		return ratelimit.CategoryLogByte
	case ratelimit.CategoryTraceMetric:
		return ratelimit.CategoryTraceMetricByte
	default:
		return ratelimit.CategoryAttachment
	}
}

// Discard is what dropping an envelope discards: its item's counts and the client report attached to it.
type Discard struct {
	Counts DiscardCounts
	Report *report.ClientReport
}

// Record records d.Counts for reason, and the attached report's outcomes again under their own reasons.
func (d Discard) Record(reports *report.Aggregator, reason report.DiscardReason) {
	d.Counts.Record(reports, reason)
	if d.Report == nil {
		return
	}
	for _, event := range d.Report.DiscardedEvents {
		reports.Record(event.Reason, event.Category, event.Quantity)
	}
}

type discardKey struct{}

// WithDiscard returns a context that carries d to the transport.
func WithDiscard(ctx context.Context, d Discard) context.Context {
	return context.WithValue(ctx, discardKey{}, d)
}

// DiscardFrom returns the Discard carried by ctx. It is empty, and records nothing, when ctx carries none.
func DiscardFrom(ctx context.Context) Discard {
	d, _ := ctx.Value(discardKey{}).(Discard)
	return d
}
