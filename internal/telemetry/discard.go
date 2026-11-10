package telemetry

import (
	"context"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/report"
)

// Indices into DiscardCounts, one per data category.
const (
	CountError = iota
	CountTransaction
	CountSpan // including the transaction itself
	CountMonitor
	CountLog
	CountLogByte
	CountTraceMetric
	CountTraceMetricByte
	CountAttachment // bytes
	countCategories
)

var categories = [countCategories]ratelimit.Category{
	CountError:           ratelimit.CategoryError,
	CountTransaction:     ratelimit.CategoryTransaction,
	CountSpan:            ratelimit.CategorySpan,
	CountMonitor:         ratelimit.CategoryMonitor,
	CountLog:             ratelimit.CategoryLog,
	CountLogByte:         ratelimit.CategoryLogByte,
	CountTraceMetric:     ratelimit.CategoryTraceMetric,
	CountTraceMetricByte: ratelimit.CategoryTraceMetricByte,
	CountAttachment:      ratelimit.CategoryAttachment,
}

// DiscardCounts holds the client report quantity per data category for a dropped item, indexed by the Count constants.
type DiscardCounts [countCategories]uint64

// Add returns c with the counts of other added.
func (c DiscardCounts) Add(other DiscardCounts) DiscardCounts {
	for i, quantity := range other {
		c[i] += quantity
	}
	return c
}

// Record records c as discarded for reason.
func (c DiscardCounts) Record(reports *report.Aggregator, reason report.DiscardReason) {
	for i, quantity := range c {
		reports.Record(reason, categories[i], quantity)
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
