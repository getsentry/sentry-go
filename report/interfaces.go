package report

import (
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// ClientReportRecorder is used by components that need to record lost/discarded events.
type ClientReportRecorder interface {
	Record(reason DiscardReason, category ratelimit.Category, quantity int64)
	RecordOne(reason DiscardReason, category ratelimit.Category)
	RecordForEnvelope(reason DiscardReason, envelope *protocol.Envelope)
	RecordItem(reason DiscardReason, item protocol.TelemetryItem)
}

// ClientReportProvider is used by the single component responsible for sending client reports.
type ClientReportProvider interface {
	TakeReport() *ClientReport
	AttachToEnvelope(envelope *protocol.Envelope)
}
