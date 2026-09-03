package traceutils

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
)

// OutgoingHeaders selects the Sentry trace, merges existing baggage, and
// derives W3C traceparent from that same selection.
func OutgoingHeaders(ctx context.Context, existing string, withTraceparent bool) (trace, baggage, traceparent string) {
	ctx = sentry.ContextWithClient(ctx, sentry.ClientFromContext(ctx))
	trace = sentry.GetTraceparent(ctx)
	if trace == "" {
		return "", "", ""
	}
	parts := strings.SplitN(trace, "-", 3)
	baggage = sentry.GetBaggage(ctx)
	if baggage != "" {
		dsc, err := sentry.DynamicSamplingContextFromHeader([]byte(baggage))
		if err != nil || !strings.EqualFold(dsc.Entries["trace_id"], parts[0]) {
			baggage = ""
		}
	}
	if existing != "" {
		baggage, _ = sentry.MergeBaggage(existing, baggage)
	}
	if withTraceparent {
		flags := "00"
		if len(parts) == 3 && parts[2] == "1" {
			flags = "01"
		}
		traceparent = "00-" + parts[0] + "-" + parts[1] + "-" + flags
	}
	return trace, baggage, traceparent
}
