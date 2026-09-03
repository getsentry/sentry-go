package traceutils

import (
	"net/http"
	"strings"

	"github.com/getsentry/sentry-go"
)

// ContinueFromRequest extracts all baggage values for a new root span.
func ContinueFromRequest(r *http.Request) sentry.SpanOption {
	return sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), strings.Join(r.Header.Values(sentry.SentryBaggageHeader), ","))
}
