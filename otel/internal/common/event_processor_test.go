package common

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func TestLinkTraceContextToErrorEventSetsOTelIDs(t *testing.T) {
	t.Parallel()

	traceID := trace.TraceID{0xd4, 0xcd, 0xa9, 0x5b, 0x65, 0x2f, 0x4a, 0x15, 0x92, 0xb4, 0x49, 0xd5, 0x92, 0x9f, 0xda, 0x1b}
	spanID := trace.SpanID{0x6e, 0x0c, 0x63, 0x25, 0x7d, 0xe3, 0x4c, 0x92}

	event := &sentry.Event{}

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	got := linkTraceContextToErrorEvent(event, &sentry.EventHint{Context: ctx})

	assert.Equal(t, map[string]any{
		"trace_id": traceID.String(),
		"span_id":  spanID.String(),
	}, got.Contexts["trace"])
}

func TestLinkTraceContextToErrorEventPreservesExistingTraceContext(t *testing.T) {
	t.Parallel()

	traceID := trace.TraceID{0xd4, 0xcd, 0xa9, 0x5b, 0x65, 0x2f, 0x4a, 0x15, 0x92, 0xb4, 0x49, 0xd5, 0x92, 0x9f, 0xda, 0x1b}
	spanID := trace.SpanID{0x6e, 0x0c, 0x63, 0x25, 0x7d, 0xe3, 0x4c, 0x92}

	event := &sentry.Event{
		Contexts: map[string]map[string]any{
			"trace": {
				"trace_id": "123",
				"span_id":  "456",
				"op":       "http.server",
			},
		},
	}

	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	got := linkTraceContextToErrorEvent(event, &sentry.EventHint{Context: ctx})

	assert.Equal(t, map[string]any{
		"trace_id": traceID.String(),
		"span_id":  spanID.String(),
		"op":       "http.server",
	}, got.Contexts["trace"])
}

func TestLinkTraceContextToErrorEventSkipsInvalidSpanContext(t *testing.T) {
	t.Parallel()

	event := &sentry.Event{}
	got := linkTraceContextToErrorEvent(event, &sentry.EventHint{Context: context.Background()})

	_, found := got.Contexts["trace"]
	assert.False(t, found)
}
