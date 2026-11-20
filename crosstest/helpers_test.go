package crosstest

import (
	"context"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
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
			Integrations: func(integrations []sentry.Integration) []sentry.Integration {
				return append(integrations, sentryotel.NewOtelIntegration())
			},
		}),
	}
}

func linkedErrorEvent(traceID sentry.TraceID, spanID sentry.SpanID, message string) linkedSignal {
	return linkedSignal{Kind: "event", TraceID: traceID.String(), SpanID: spanID.String(), Message: message}
}

func linkedLogEvent(traceID sentry.TraceID, spanID sentry.SpanID, body string) linkedSignal {
	return linkedSignal{Kind: "log", TraceID: traceID.String(), SpanID: spanID.String(), Level: sentry.LogLevelInfo, Message: body}
}

func linkedMetricEvent(traceID sentry.TraceID, spanID sentry.SpanID, name string, value int64) linkedSignal {
	return linkedSignal{
		Kind:       "metric",
		TraceID:    traceID.String(),
		SpanID:     spanID.String(),
		MetricType: sentry.MetricTypeCounter,
		Name:       name,
		Value:      fmt.Sprint(value),
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

func requireLinked(t *testing.T, f *sentrytest.Fixture, want ...linkedSignal) {
	t.Helper()

	diff := cmp.Diff(sortLinkedSignals(want), sortLinkedSignals(linkedSignals(f)))
	if diff != "" {
		t.Fatalf("linked payload mismatch (-want +got):\n%s", diff)
	}
}

func linkedSignals(f *sentrytest.Fixture) []linkedSignal {
	var signals []linkedSignal

	for _, event := range f.Events() {
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
	}

	for _, batch := range f.Transport.LogBatches() {
		for _, log := range batch.Logs {
			signals = append(signals, linkedSignal{
				Kind:    "log",
				TraceID: log.TraceID.String(),
				SpanID:  log.SpanID.String(),
				Level:   log.Level,
				Message: log.Body,
			})
		}
	}

	for _, batch := range f.Transport.MetricBatches() {
		for _, metric := range batch.Metrics {
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

	return signals
}

func sortLinkedSignals(signals []linkedSignal) []linkedSignal {
	slices.SortStableFunc(signals, func(a, b linkedSignal) int {
		return strings.Compare(linkedSignalSortKey(a), linkedSignalSortKey(b))
	})
	return signals
}

func linkedTraceContext(event *sentry.Event) (string, string) {
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
