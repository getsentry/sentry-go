package sentryotel

import (
	"errors"
	"fmt"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestLinkTraceContextToErrorEventSetsContext(t *testing.T) {

	withExistingContextOptions := []bool{false, true}

	for _, withExistingContext := range withExistingContextOptions {
		withExistingContext := withExistingContext
		name := fmt.Sprintf("withExistingContext_%v", withExistingContext)

		t.Run(name, func(t *testing.T) {
			_, _, tracer := setupSpanProcessorTest()
			ctx, otelSpan := tracer.Start(emptyContextWithSentry(), "spanName")
			sentrySpan, _ := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())

			hub := sentry.GetHubFromContext(ctx)
			client, scope := hub.Client(), hub.Scope()

			if withExistingContext {
				// The existing "trace" context should be ovewritten by the event processor
				scope.SetContext("trace", map[string]interface{}{"trace_id": "123"})
			}
			client.CaptureException(
				errors.New("new sentry exception with existing trace context"),
				&sentry.EventHint{Context: ctx},
				hub.Scope(),
			)

			transport := client.Transport.(*TransportMock)
			events := transport.Events()
			assertEqual(t, len(events), 1)
			err := events[0]
			exception := err.Exception[0]
			assertEqual(t, exception.Type, "*errors.errorString")
			assertEqual(t, exception.Value, "new sentry exception with existing trace context")
			assertEqual(t, err.Type, "")
			assertEqual(t,
				err.Contexts["trace"],
				map[string]interface{}{
					"trace_id":       sentrySpan.TraceID.String(),
					"span_id":        sentrySpan.SpanID.String(),
					"parent_span_id": sentrySpan.ParentSpanID.String(),
				},
			)
		})
	}
}
