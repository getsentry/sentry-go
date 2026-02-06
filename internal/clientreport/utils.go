package clientreport

import (
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// RecordForEnvelope records client report outcomes for all items in the envelope.
// It inspects envelope item headers to derive categories, span counts, and log byte sizes.
func RecordForEnvelope(reason DiscardReason, envelope *protocol.Envelope) {
	for _, item := range envelope.Items {
		if item == nil || item.Header == nil {
			continue
		}
		switch item.Header.Type {
		case protocol.EnvelopeItemTypeEvent:
			RecordOne(reason, ratelimit.CategoryError)
		case protocol.EnvelopeItemTypeTransaction:
			RecordOne(reason, ratelimit.CategoryTransaction)
			spanCount := int64(item.Header.SpanCount)
			Record(reason, ratelimit.CategorySpan, spanCount)
		case protocol.EnvelopeItemTypeLog:
			if item.Header.ItemCount != nil {
				Record(reason, ratelimit.CategoryLog, int64(*item.Header.ItemCount))
			}
			if item.Header.Length != nil {
				Record(reason, ratelimit.CategoryLogByte, int64(*item.Header.Length))
			}
		case protocol.EnvelopeItemTypeCheckIn:
			RecordOne(reason, ratelimit.CategoryMonitor)
		case protocol.EnvelopeItemTypeAttachment, protocol.EnvelopeItemTypeClientReport:
			// Skip — not reportable categories
		}
	}
}

// RecordItem records outcomes for a telemetry item, including supplementary
// categories (span outcomes for transactions, byte size for logs).
func RecordItem(reason DiscardReason, item protocol.TelemetryItem) {
	category := item.GetCategory()
	RecordOne(reason, category)

	// Span outcomes for transactions
	if category == ratelimit.CategoryTransaction {
		type spanCounter interface{ GetSpanCount() int }
		if sc, ok := item.(spanCounter); ok {
			if count := sc.GetSpanCount(); count > 0 {
				Record(reason, ratelimit.CategorySpan, int64(count))
			}
		}
	}

	// Byte size outcomes for logs
	if category == ratelimit.CategoryLog {
		type sizer interface{ ApproximateSize() int }
		if s, ok := item.(sizer); ok {
			if size := s.ApproximateSize(); size > 0 {
				Record(reason, ratelimit.CategoryLogByte, int64(size))
			}
		}
	}
}

// AttachToEnvelope adds a client report to the envelope if the aggregator has outcomes available.
func AttachToEnvelope(envelope *protocol.Envelope) {
	r := TakeReport()
	if r != nil {
		rItem, err := r.ToEnvelopeItem()
		if err == nil {
			envelope.AddItem(rItem)
		} else {
			debuglog.Printf("failed to serialize client report: %v, with err: %v", r, err)
		}
	}
}
