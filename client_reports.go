package sentry

import (
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/report"
)

// clientReportRecorder records the byte size of dropped logs and metrics alongside the aggregator's outcomes.
type clientReportRecorder struct {
	*report.Aggregator
}

func (r clientReportRecorder) RecordItem(reason report.DiscardReason, item report.ReportableItem) {
	r.Aggregator.RecordItem(reason, item)
	switch item := item.(type) {
	case *Log:
		r.Record(reason, ratelimit.CategoryLogByte, int64(item.byteSize()))
	case *Metric:
		r.Record(reason, ratelimit.CategoryTraceMetricByte, int64(item.byteSize()))
	}
}
