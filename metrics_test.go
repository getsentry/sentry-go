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
		metricsFunc func(ctx context.Context, m Meter[float64])
		wantEvents  []Event
	}{
		{
			name: "count",
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			metricsFunc: func(ctx context.Context, m Meter[float64]) {
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
			meter := NewMeter[float64](ctx)

			tt.metricsFunc(ctx, meter)
			Flush(testutils.FlushTimeout())

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
	meter := NewMeter[float64](ctx)
	meter.Count("test.count", 42, MeterOptions{})
	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func Test_batchMeter_FlushWithContext(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter[float64](ctx)
	meter.Count("test.count", 42, MeterOptions{})

	cancelCtx, cancel := context.WithTimeout(context.Background(), testutils.FlushTimeout())
	FlushWithContext(cancelCtx)
	defer cancel()

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

	meter := NewMeter[int](ctx)
	meter.Count("test.count", 1, MeterOptions{})
	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func Test_Meter_ExceedBatchSize(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	meter := NewMeter[int](ctx)
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
