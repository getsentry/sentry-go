package sentry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

func TestClientReports_CustomTransport(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		err      error
		reason   report.DiscardReason
		disabled bool
	}{
		{"queue rejection", ErrTransportQueueFull, report.ReasonQueueOverflow, false},
		{"send rejection", errors.New("rejected"), report.ReasonSendError, false},
		{"disabled", ErrTransportQueueFull, report.ReasonQueueOverflow, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				DisableClientReports: tt.disabled,
				Transport: &wrappedTransport{Transport: &NoopTransport{}, send: func(context.Context, *protocol.Envelope) error {
					return tt.err
				}},
				BeforeSend: func(event *Event, _ *EventHint) *Event {
					if event.Message == "drop" {
						return nil
					}
					return event
				},
			})
			require.NoError(t, err)
			defer client.Close()
			ctx, _ := WithScope(context.Background())
			client.CaptureMessage(ctx, "drop")
			require.NotNil(t, client.CaptureMessage(ctx, "send"))
			client.Flush(testutils.FlushTimeout())
			pending := client.reports.TakeReport()
			if tt.disabled {
				require.Nil(t, pending)
				return
			}
			require.NotNil(t, pending)
			require.ElementsMatch(t, []report.DiscardedEvent{
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
				{Reason: tt.reason, Category: ratelimit.CategoryError, Quantity: 1},
			}, pending.DiscardedEvents)
		})
	}
}

// TestClientReports_Integration tests that client reports are properly generated
// and sent when events are dropped for various reasons.
func TestClientReports_Integration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		reportOnly bool
		disabled   bool
		status     int
		transport  Transport
	}{
		{"attached", false, false, http.StatusOK, nil},
		{"report-only flush", true, false, http.StatusOK, nil},
		{"disabled", true, true, http.StatusOK, nil},
		{"failed report is retained without retrying", true, false, http.StatusInternalServerError, nil},
		{"wrapped attached", false, false, http.StatusOK, &wrappedTransport{Transport: NewHTTPSyncTransport()}},
		{"wrapped disabled", true, true, http.StatusOK, &wrappedTransport{Transport: NewHTTPSyncTransport()}},
		{"wrapped failed report", true, false, http.StatusInternalServerError, &wrappedTransport{Transport: NewHTTPSyncTransport()}},
		{"wrapped failed event", false, false, http.StatusInternalServerError, &wrappedTransport{Transport: NewHTTPSyncTransport()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBodies [][]byte
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				receivedBodies = append(receivedBodies, body)
				mu.Unlock()
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"id":"test-event-id"}`))
			}))
			defer srv.Close()

			dsn := strings.Replace(srv.URL, "//", "//test@", 1) + "/1"
			var logSize, metricSize int64
			c, err := NewClient(ClientOptions{
				Dsn:                  dsn,
				Transport:            tt.transport,
				DisableClientReports: tt.disabled,
				SampleRate:           1.0,
				BeforeSend: func(event *Event, _ *EventHint) *Event {
					if event.Message == "drop-me" {
						event.Attachments = nil
						return nil
					}
					return event
				},
				BeforeSendTransaction: func(event *Event, _ *EventHint) *Event {
					event.Spans = nil
					return nil
				},
				BeforeSendLog: func(log *Log) *Log {
					logSize = byteSize(len(log.Body), log.Attributes)
					log.Attributes = nil
					return nil
				},
				BeforeSendMetric: func(metric *Metric) *Metric {
					metricSize = byteSize(len(metric.Name)+8, metric.Attributes)
					metric.Attributes = nil
					return nil
				},
			})
			require.NoError(t, err)
			defer c.Close()

			// A second client's disabled reports must not affect the first client.
			disabled, err := NewClient(ClientOptions{Dsn: testDsn, DisableClientReports: true})
			require.NoError(t, err)
			defer disabled.Close()

			ctx, _ := WithScope(context.Background())
			ctx = ContextWithClient(ctx, c)
			c.CaptureEvent(ctx, &Event{Message: "drop-me", Attachments: []*Attachment{{Payload: []byte("abc")}}})
			c.CaptureEvent(ctx, &Event{Type: transactionType, Spans: []*Span{{}, {}}})
			NewLogger(ctx).Info().String("key", "value").Emit("drop-me")
			NewMeter(ctx).Count("drop-me", 1)
			processorCtx, processorScope := WithScope(ctx)
			processorScope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
				if event.Message == "processor-drop" {
					event.Attachments = nil
					return nil
				}
				return event
			})
			c.CaptureEvent(processorCtx, &Event{Message: "processor-drop", Attachments: []*Attachment{{Payload: []byte("abc")}}})
			if !tt.reportOnly {
				c.CaptureMessage(ctx, "hi")
			}
			require.True(t, c.Flush(testutils.FlushTimeout()))

			if tt.disabled {
				mu.Lock()
				count := len(receivedBodies)
				mu.Unlock()
				require.Zero(t, count)
				require.Nil(t, c.reports.TakeReport())
				return
			}
			var got report.ClientReport
			require.Eventually(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				for _, body := range receivedBodies {
					for line := range bytes.SplitSeq(body, []byte("\n")) {
						var candidate report.ClientReport
						if json.Unmarshal(line, &candidate) == nil && len(candidate.DiscardedEvents) > 0 {
							got = candidate
							return true
						}
					}
				}
				return false
			}, time.Second, 10*time.Millisecond, "no client report received")
			if tt.reportOnly {
				mu.Lock()
				count := len(receivedBodies)
				mu.Unlock()
				require.Equal(t, 1, count, "flush must not retry failed reports")
			}
			pending := c.reports.TakeReport()
			require.False(t, got.Timestamp.IsZero(), "client report missing timestamp")
			want := []report.DiscardedEvent{
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryAttachment, Quantity: 3},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryTransaction, Quantity: 1},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategorySpan, Quantity: 3},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryLog, Quantity: 1},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryLogByte, Quantity: logSize},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryTraceMetric, Quantity: 1},
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryTraceMetricByte, Quantity: metricSize},
				{Reason: report.ReasonEventProcessor, Category: ratelimit.CategoryError, Quantity: 1},
				{Reason: report.ReasonEventProcessor, Category: ratelimit.CategoryAttachment, Quantity: 3},
			}
			require.ElementsMatch(t, want, got.DiscardedEvents)
			if tt.status == http.StatusOK {
				require.Nil(t, pending)
			} else {
				require.NotNil(t, pending)
				if !tt.reportOnly {
					want = append(want, report.DiscardedEvent{Reason: report.ReasonSendError, Category: ratelimit.CategoryError, Quantity: 1})
				}
				require.ElementsMatch(t, want, pending.DiscardedEvents)
			}
		})
	}
}

// Discard counts follow Relay's size rules.
func TestClientReports_DiscardCounts(t *testing.T) {
	t.Parallel()
	attrs := map[string]attribute.Value{
		"bool":    attribute.BoolValue(false),                      // 4 + 1
		"int":     attribute.Int64Value(math.MinInt64),             // 3 + 8
		"uint":    attribute.Uint64Value(math.MaxUint64),           // 4 + 8
		"float":   attribute.Float64Value(1e-9),                    // 5 + 8
		"string":  attribute.StringValue("\"<é>\"\n"),              // 6 + 7, escaped when serialized
		"bools":   attribute.BoolSliceValue([]bool{true, false}),   // 5 + 2
		"ints":    attribute.Int64SliceValue([]int64{1, 22}),       // 4 + 16
		"floats":  attribute.Float64SliceValue([]float64{1.5}),     // 6 + 8
		"strings": attribute.StringSliceValue([]string{"a", "bc"}), // 7 + 3
	}
	const attrsSize = 5 + 11 + 12 + 13 + 13 + 7 + 20 + 14 + 10

	tests := []struct {
		name string
		item telemetry.Item
		want telemetry.DiscardCounts
	}{
		{"empty log", &Log{},
			telemetry.DiscardCounts{Category: ratelimit.CategoryLog, Quantity: 1, Bytes: 1}},
		{"log", &Log{Body: "user logged in", Level: LogLevelInfo, Timestamp: time.Now(), Attributes: attrs},
			telemetry.DiscardCounts{Category: ratelimit.CategoryLog, Quantity: 1, Bytes: 14 + attrsSize}},
		{"metric", &Metric{Name: "http.requests", Type: MetricTypeCounter, Value: Int64MetricValue(7), Attributes: attrs},
			telemetry.DiscardCounts{Category: ratelimit.CategoryTraceMetric, Quantity: 1, Bytes: 13 + 8 + attrsSize}},
		{"transaction", &Event{Type: transactionType, Spans: []*Span{{}, {}}},
			telemetry.DiscardCounts{Category: ratelimit.CategoryTransaction, Quantity: 1, Spans: 3}},
		{"error with attachments", &Event{Attachments: []*Attachment{{Payload: []byte("abc")}, {Payload: []byte("de")}}},
			telemetry.DiscardCounts{Category: ratelimit.CategoryError, Quantity: 1, Bytes: 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, tt.item.DiscardCounts()); diff != "" {
				t.Errorf("DiscardCounts() (-want +got):\n%s", diff)
			}
		})
	}
}
