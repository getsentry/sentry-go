package clientreport

import (
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
)

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
