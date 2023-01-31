//go:build go1.18

package sentryotel

import (
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestLinkTraceContextToErrorEventWithEmptyTraceContext(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	ctx, otelSpan := tracer.Start(emptyContextWithSentry(), "spanName")
	sentrySpan, _ := sentrySpanMap.Get(otelSpan.SpanContext().SpanID())

	hub := sentry.GetHubFromContext(ctx)
	client := hub.Client()
	client.CaptureException(
		errors.New("new sentry exception"),
		&sentry.EventHint{Context: ctx},
		nil,
	)

	transport := client.Transport.(*TransportMock)
	events := transport.Events()
	assertEqual(t, len(events), 1)
	err := events[0]
	exception := err.Exception[0]
	assertEqual(t, exception.Type, "*errors.errorString")
	assertEqual(t, exception.Value, "new sentry exception")
	assertEqual(t, err.Type, "")
	assertEqual(t,
		err.Contexts["trace"],
		map[string]interface{}{
			"trace_id":       sentrySpan.TraceID.String(),
			"span_id":        sentrySpan.SpanID.String(),
			"parent_span_id": sentrySpan.ParentSpanID.String(),
		},
	)
}

func TestLinkTraceContextToErrorEventDoesNotTouchExistingTraceContext(t *testing.T) {
	_, _, tracer := setupSpanProcessorTest()
	ctx, _ := tracer.Start(emptyContextWithSentry(), "spanName")

	hub := sentry.GetHubFromContext(ctx)
	hub.Scope().SetContext("trace", map[string]interface{}{"trace_id": "123"})
	client := hub.Client()
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
		map[string]interface{}{"trace_id": "123"},
	)
}
