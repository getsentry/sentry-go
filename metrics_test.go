package sentry

import (
	"context"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func Test_sentryMeter_Methods(t *testing.T) {
	attrs := map[string]Attribute{
		"sentry.release":        {Value: "v1.2.3", Type: "string"},
		"sentry.environment":    {Value: "testing", Type: "string"},
		"sentry.server.address": {Value: "test-server", Type: "string"},
		"sentry.sdk.name":       {Value: "sentry.go", Type: "string"},
		"sentry.sdk.version":    {Value: "0.10.0", Type: "string"},
	}

	tests := []struct {
		name        string
		metricsFunc func(m Meter)
		wantEvents  []Event
	}{
		{
			name: "count",
			metricsFunc: func(m Meter) {
				m.Count("test.count", 5, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.count",
							Value:   5,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeCounter,
							Unit: "",
						},
					},
				},
			},
		},
		{
			name: "distribution",
			metricsFunc: func(m Meter) {
				m.Distribution("test.distribution", 3.14, MeterOptions{
					Attributes: []attribute.Builder{attribute.Int("key.int", 42)},
					Unit:       "ms",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.distribution",
							Value:   3.14,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.int": {Value: int64(42), Type: "integer"},
							}),
							Type: MetricTypeDistribution,
							Unit: "ms",
						},
					},
				},
			},
		},
		{
			name: "gauge",
			metricsFunc: func(m Meter) {
				m.Gauge("test.gauge", 2.71, MeterOptions{
					Attributes: []attribute.Builder{attribute.Float64("key.float", 1.618)},
					Unit:       "requests",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.gauge",
							Value:   2.71,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.float": {Value: 1.618, Type: "double"},
							}),
							Type: MetricTypeGauge,
							Unit: "requests",
						},
					},
				},
			},
		},
		{
			name: "zero count",
			metricsFunc: func(m Meter) {
				m.Count("test.zero.count", 0, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.count",
							Value:   0,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeCounter,
							Unit: "",
						},
					},
				},
			},
		},
		{
			name: "zero distribution",
			metricsFunc: func(m Meter) {
				m.Distribution("test.zero.distribution", 0, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
					Unit:       "bytes",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.distribution",
							Value:   0,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeDistribution,
							Unit: "bytes",
						},
					},
				},
			},
		},
		{
			name: "zero gauge",
			metricsFunc: func(m Meter) {
				m.Gauge("test.zero.gauge", 0, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
					Unit:       "connections",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.gauge",
							Value:   0,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeGauge,
							Unit: "connections",
						},
					},
				},
			},
		},
		{
			name: "negative count",
			metricsFunc: func(m Meter) {
				m.Count("test.negative.count", -10, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.count",
							Value:   -10,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeCounter,
							Unit: "",
						},
					},
				},
			},
		},
		{
			name: "negative distribution",
			metricsFunc: func(m Meter) {
				m.Distribution("test.negative.distribution", -2.5, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
					Unit:       "ms",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.distribution",
							Value:   -2.5,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeDistribution,
							Unit: "ms",
						},
					},
				},
			},
		},
		{
			name: "negative gauge",
			metricsFunc: func(m Meter) {
				m.Gauge("test.negative.gauge", -5, MeterOptions{
					Attributes: []attribute.Builder{attribute.String("key.string", "value")},
					Unit:       "connections",
				})
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.gauge",
							Value:   -5,
							Attributes: testutils.MergeMaps(attrs, map[string]Attribute{
								"key.string": {Value: "value", Type: "string"},
							}),
							Type: MetricTypeGauge,
							Unit: "connections",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := setupMockTransport()
			meter := NewMeter(ctx)

			tt.metricsFunc(meter)
			flushFromContext(ctx, testutils.FlushTimeout())

			opts := cmp.Options{cmpopts.IgnoreFields(Metric{}, "Timestamp")}

			gotEvents := mockTransport.Events()
			if len(gotEvents) != len(tt.wantEvents) {
				t.Fatalf("got %d events, want %d", len(gotEvents), len(tt.wantEvents))
			}

			for i, event := range gotEvents {
				assertEqual(t, event.Type, traceMetricEvent.Type)
				if diff := cmp.Diff(tt.wantEvents[i].Metrics, event.Metrics, opts); diff != "" {
					t.Errorf("event[%d] Metrics mismatch (-want +got):\n%s", i, diff)
				}
				mockTransport.events = nil
			}
		})
	}
}

func Test_batchMeter_Flush(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)
	meter.Count("test.count", 42, MeterOptions{})
	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func Test_batchMeter_FlushWithContext(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)
	meter.Count("test.count", 42, MeterOptions{})

	cancelCtx, cancel := context.WithTimeout(context.Background(), testutils.FlushTimeout())
	defer cancel()
	hub := GetHubFromContext(ctx)
	hub.FlushWithContext(cancelCtx)

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func Test_sentryMeter_BeforeSendMetric(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableMetrics: true,
		EnableTracing: true,
		BeforeSendMetric: func(_ *Metric) *Metric {
			return nil
		},
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	ctx = SetHubOnContext(ctx, hub)

	meter := NewMeter(ctx)
	meter.Count("test.count", 1, MeterOptions{})
	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func Test_Meter_ExceedBatchSize(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)
	for i := 0; i < batchSize; i++ {
		meter.Count("test.count", 1, MeterOptions{})
	}

	// sleep to wait for the batch to be processed
	time.Sleep(time.Millisecond * 20)
	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected only one event with 100 metrics, got %d", len(events))
	}
}

func Test_batchMeter_FlushMultipleTimes(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)

	for i := 0; i < 5; i++ {
		meter.Count("test.count", 1, MeterOptions{})
	}

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after first flush, got %d", len(events))
	}
	if len(events[0].Metrics) != 5 {
		t.Fatalf("expected 5 metrics in first batch, got %d", len(events[0].Metrics))
	}

	mockTransport.events = nil

	for i := 0; i < 3; i++ {
		meter.Count("test.count", 1, MeterOptions{})
	}

	flushFromContext(ctx, testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after second flush, got %d", len(events))
	}
	if len(events[0].Metrics) != 3 {
		t.Fatalf("expected 3 metrics in second batch, got %d", len(events[0].Metrics))
	}

	mockTransport.events = nil

	flushFromContext(ctx, testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after third flush with no metrics, got %d", len(events))
	}
}

func Test_batchMeter_Shutdown(t *testing.T) {
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:                    testDsn,
		Transport:              mockTransport,
		EnableMetrics:          true,
		DisableTelemetryBuffer: true,
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx := SetHubOnContext(context.Background(), hub)
	meter := NewMeter(ctx)
	for i := 0; i < 3; i++ {
		meter.Count("test.count", 1, MeterOptions{})
	}

	hub = GetHubFromContext(ctx)
	hub.Client().batchMeter.Shutdown()

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after shutdown, got %d", len(events))
	}
	if len(events[0].Metrics) != 3 {
		t.Fatalf("expected 3 metrics in shutdown batch, got %d", len(events[0].Metrics))
	}

	mockTransport.events = nil

	// Test that shutdown can be called multiple times safely
	hub.Client().batchMeter.Shutdown()
	hub.Client().batchMeter.Shutdown()

	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after multiple shutdowns, got %d", len(events))
	}

	flushFromContext(ctx, testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after flush on shutdown meter, got %d", len(events))
	}
}

func Test_sentryMeter_TracePropagationWithTransaction(t *testing.T) {
	ctx, mockTransport := setupMockTransport()

	// Start a new transaction
	txn := StartTransaction(ctx, "test-transaction")
	defer txn.Finish()

	expectedTraceID := txn.TraceID
	expectedSpanID := txn.SpanID

	meter := NewMeter(txn.Context())
	meter.Count("test.count", 42, MeterOptions{})

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	metrics := events[0].Metrics
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]

	if metric.TraceID != expectedTraceID {
		t.Errorf("unexpected TraceID: got %s, want %s", metric.TraceID.String(), expectedTraceID.String())
	}
	if metric.SpanID != expectedSpanID {
		t.Errorf("unexpected SpanID: got %s, want %s", metric.SpanID.String(), expectedSpanID.String())
	}
}

func Test_sentryMeter_UserAttributes(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableMetrics: true,
		EnableTracing: true,
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub().Clone()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	hub.Scope().SetUser(User{
		ID:    "user123",
		Name:  "Test User",
		Email: "test@example.com",
	})

	ctx = SetHubOnContext(ctx, hub)

	meter := NewMeter(ctx)
	meter.Count("test.count", 1, MeterOptions{})
	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	metrics := events[0].Metrics
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	metric := metrics[0]
	attrs := metric.Attributes

	if val, ok := attrs["user.id"]; !ok {
		t.Error("missing user.id attribute")
	} else if val.Value != "user123" {
		t.Errorf("unexpected user.id: got %v, want %v", val.Value, "user123")
	}

	if val, ok := attrs["user.name"]; !ok {
		t.Error("missing user.name attribute")
	} else if val.Value != "Test User" {
		t.Errorf("unexpected user.name: got %v, want %v", val.Value, "Test User")
	}

	if val, ok := attrs["user.email"]; !ok {
		t.Error("missing user.email attribute")
	} else if val.Value != "test@example.com" {
		t.Errorf("unexpected user.email: got %v, want %v", val.Value, "test@example.com")
	}
}

func Test_sentryMeter_SetAttributes(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)
	meter.SetAttributes(
		attribute.String("key.string", "some str"),
		attribute.Int("key.int", 42),
		attribute.Int64("key.int64", 17),
		attribute.Float64("key.float", 42.2),
		attribute.Bool("key.bool", true),
	)
	meter.Count("test.count", 1, MeterOptions{})

	flushFromContext(ctx, testutils.FlushTimeout())

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	if len(gotEvents[0].Metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(gotEvents[0].Metrics))
	}

	metric := gotEvents[0].Metrics[0]
	attrs := metric.Attributes

	assertEqual(t, attrs["key.string"].Value, "some str")
	assertEqual(t, attrs["key.int"].Value, int64(42))
	assertEqual(t, attrs["key.int64"].Value, int64(17))
	assertEqual(t, attrs["key.float"].Value, 42.2)
	assertEqual(t, attrs["key.bool"].Value, true)
}

func Test_sentryMeter_SetAttributes_Persistence(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)
	meter.SetAttributes(attribute.Int("int", 42))
	meter.Count("test.count1", 1, MeterOptions{})

	meter.SetAttributes(attribute.String("string", "some str"))
	meter.Count("test.count2", 2, MeterOptions{})
	flushFromContext(ctx, testutils.FlushTimeout())

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Metrics[0].Attributes["int"].Value, int64(42))
	if _, ok := event.Metrics[1].Attributes["int"]; !ok {
		t.Fatalf("expected key to exist")
	}
	assertEqual(t, event.Metrics[1].Attributes["string"].Value, "some str")
}

func TestNewMeter_DisabledMetrics(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		EnableMetrics: false, // Disabled
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx = SetHubOnContext(ctx, hub)

	meter := NewMeter(ctx)
	meter.Count("test.count", 1, MeterOptions{})
	meter.Gauge("test.gauge", 2.5, MeterOptions{})
	meter.Distribution("test.dist", 3.14, MeterOptions{})

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events with disabled metrics, got %d", len(events))
	}
}

func Test_sentryMeter_EmptyName(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter(ctx)

	meter.Count("", 1, MeterOptions{})
	meter.Gauge("", 2.5, MeterOptions{})
	meter.Distribution("", 3.14, MeterOptions{})

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events with empty name, got %d", len(events))
	}
}

func Test_noopMeter_Methods(_ *testing.T) {
	ctx := context.Background()
	meter := NewMeter(ctx)

	meter.Count("test.count", 1, MeterOptions{})
	meter.Gauge("test.gauge", 2.5, MeterOptions{})
	meter.Distribution("test.dist", 3.14, MeterOptions{})
	meter.SetAttributes(attribute.String("key", "value"))
}
