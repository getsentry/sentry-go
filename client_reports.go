package sentry

import (
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
)

// DiscardCounts returns client report counts for a dropped event.
func (e *Event) DiscardCounts() telemetry.DiscardCounts {
	var counts telemetry.DiscardCounts
	switch e.Type {
	case transactionType:
		counts = transactionDiscardCounts(uint64(len(e.Spans)))
	case logEvent.Type:
		counts[ratelimit.CountLog] = 1
	case traceMetricEvent.Type:
		counts[ratelimit.CountTraceMetric] = 1
	default:
		counts[ratelimit.CountError] = 1
	}
	for _, attachment := range e.Attachments {
		counts[ratelimit.CountAttachment] += uint64(len(attachment.Payload))
	}
	return counts
}

// transactionDiscardCounts returns client report counts for a dropped transaction with the given child spans.
func transactionDiscardCounts(spans uint64) telemetry.DiscardCounts {
	return telemetry.DiscardCounts{ratelimit.CountTransaction: 1, ratelimit.CountSpan: spans + 1}
}

// DiscardCounts returns client report counts for a dropped check-in.
func (c *checkInItem) DiscardCounts() telemetry.DiscardCounts {
	return telemetry.DiscardCounts{ratelimit.CountMonitor: 1}
}

// DiscardCounts returns client report counts for a dropped log.
func (l *Log) DiscardCounts() telemetry.DiscardCounts {
	return telemetry.DiscardCounts{ratelimit.CountLog: 1, ratelimit.CountLogByte: byteSize(len(l.Body), l.Attributes)}
}

// DiscardCounts returns client report counts for a dropped metric.
func (m *Metric) DiscardCounts() telemetry.DiscardCounts {
	return telemetry.DiscardCounts{
		ratelimit.CountTraceMetric:     1,
		ratelimit.CountTraceMetricByte: byteSize(len(m.Name)+valueSize(m.Value.value), m.Attributes),
	}
}

// byteSize returns the byte count of a log or metric, following Relay:
// https://github.com/getsentry/relay/blob/78ba7825b26af2210029e831897e4c4545292294/relay-event-normalization/src/eap/size.rs#L15-L46
func byteSize(size int, attributes map[string]attribute.Value) uint64 {
	for key, value := range attributes {
		size += len(key) + valueSize(value)
	}
	return uint64(max(size, 1))
}

// valueSize returns the byte count of an attribute value, following Relay.
func valueSize(v attribute.Value) int {
	switch v.Type() {
	case attribute.BOOL:
		return 1
	case attribute.INT64, attribute.UINT64, attribute.FLOAT64:
		return 8
	case attribute.STRING:
		return len(v.AsString())
	case attribute.BOOLSLICE:
		return len(v.AsBoolSlice())
	case attribute.INT64SLICE:
		return 8 * len(v.AsInt64Slice())
	case attribute.FLOAT64SLICE:
		return 8 * len(v.AsFloat64Slice())
	case attribute.STRINGSLICE:
		var size int
		for _, s := range v.AsStringSlice() {
			size += len(s)
		}
		return size
	default:
		return 0
	}
}
