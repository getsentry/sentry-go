package crosstest

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	sentryotel "github.com/getsentry/sentry-go/otel"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace"
)

func fixedOTelContext() (context.Context, sentry.TraceID, sentry.SpanID) {
	var traceID sentry.TraceID
	var spanID sentry.SpanID
	if _, err := hex.Decode(traceID[:], []byte("d4cda95b652f4a1592b449d5929fda1b")); err != nil {
		panic(err)
	}
	if _, err := hex.Decode(spanID[:], []byte("6e0c63257de34c92")); err != nil {
		panic(err)
	}

	otelCtx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(traceID),
		SpanID:     trace.SpanID(spanID),
		TraceFlags: trace.FlagsSampled,
	}))

	return otelCtx, traceID, spanID
}

// otelOpts returns the common fixture options for OTel integration tests.
func otelOpts() []sentrytest.Option {
	return []sentrytest.Option{
		sentrytest.WithClientOptions(sentry.ClientOptions{
			EnableTracing:    true,
			TracesSampleRate: 1.0,
			EnableLogs:       true,
			Integrations: func(integrations []sentry.Integration) []sentry.Integration {
				return append(integrations, sentryotel.NewOtelIntegration())
			},
		}),
	}
}

func linkedErrorEvent(traceID sentry.TraceID, spanID sentry.SpanID, message string) *sentry.Event {
	return &sentry.Event{
		Contexts: map[string]sentry.Context{
			"trace": sentry.TraceContext{
				TraceID: traceID,
				SpanID:  spanID,
			}.Map(),
		},
		Exception: []sentry.Exception{{
			Value: message,
		}},
	}
}

func linkedLogEvent(traceID sentry.TraceID, spanID sentry.SpanID, body string) *sentry.Event {
	return &sentry.Event{
		Logs: []sentry.Log{{
			TraceID: traceID,
			SpanID:  spanID,
			Level:   sentry.LogLevelInfo,
			Body:    body,
		}},
	}
}

func linkedMetricEvent(traceID sentry.TraceID, spanID sentry.SpanID, name string, value int64) *sentry.Event {
	return &sentry.Event{
		Metrics: []sentry.Metric{{
			TraceID: traceID,
			SpanID:  spanID,
			Type:    sentry.MetricTypeCounter,
			Name:    name,
			Value:   sentry.Int64MetricValue(value),
		}},
	}
}

type linkedSignal struct {
	Kind       string
	TraceID    string
	SpanID     string
	Message    string
	Level      sentry.LogLevel
	MetricType sentry.MetricType
	Name       string
	Value      string
}

func requireLinked(t *testing.T, got []*sentry.Event, want ...*sentry.Event) {
	t.Helper()

	diff := cmp.Diff(normalizeLinkedSignals(want), normalizeLinkedSignals(got))
	if diff != "" {
		t.Fatalf("linked payload mismatch (-want +got):\n%s", diff)
	}
}

func normalizeLinkedSignals(events []*sentry.Event) []linkedSignal {
	signals := make([]linkedSignal, 0, len(events))

	for _, event := range events {
		if event == nil {
			continue
		}

		traceID, spanID := linkedTraceContext(event)

		message := event.Message
		if len(event.Exception) > 0 {
			message = event.Exception[0].Value
		}
		if message != "" {
			signals = append(signals, linkedSignal{
				Kind:    "event",
				TraceID: traceID,
				SpanID:  spanID,
				Message: message,
			})
		}

		for _, log := range event.Logs {
			signals = append(signals, linkedSignal{
				Kind:    "log",
				TraceID: log.TraceID.String(),
				SpanID:  log.SpanID.String(),
				Level:   log.Level,
				Message: log.Body,
			})
		}

		for _, metric := range event.Metrics {
			signals = append(signals, linkedSignal{
				Kind:       "metric",
				TraceID:    metric.TraceID.String(),
				SpanID:     metric.SpanID.String(),
				MetricType: metric.Type,
				Name:       metric.Name,
				Value:      fmt.Sprint(metric.Value.AsInterface()),
			})
		}
	}

	sort.SliceStable(signals, func(i, j int) bool {
		return linkedSignalSortKey(signals[i]) < linkedSignalSortKey(signals[j])
	})

	return signals
}

func linkedTraceContext(event *sentry.Event) (string, string) {
	if event == nil {
		return "", ""
	}

	traceCtx, ok := event.Contexts["trace"]
	if !ok {
		return "", ""
	}

	return fmt.Sprint(traceCtx["trace_id"]), fmt.Sprint(traceCtx["span_id"])
}

func linkedSignalSortKey(signal linkedSignal) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s",
		signal.Kind,
		signal.TraceID,
		signal.SpanID,
		signal.Message,
		signal.Level,
		signal.MetricType,
		signal.Name,
		signal.Value,
	)
}
