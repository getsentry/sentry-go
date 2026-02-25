package sentry

import (
	"context"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
)

func setupMetricsTest() (context.Context, *MockTransport) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableTracing: true,
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub().Clone()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	ctx = SetHubOnContext(ctx, hub)
	return ctx, mockTransport
}

func Test_sentryMeter_Methods(t *testing.T) {
	attrs := map[string]attribute.Value{
		"sentry.release":        attribute.StringValue("v1.2.3"),
		"sentry.environment":    attribute.StringValue("testing"),
		"sentry.server.address": attribute.StringValue("test-server"),
		"sentry.sdk.name":       attribute.StringValue("sentry.go"),
		"sentry.sdk.version":    attribute.StringValue("0.10.0"),
	}

	tests := []struct {
		name        string
		metricsFunc func(ctx context.Context, m Meter)
		wantEvents  []Event
	}{
		{
			name: "count",
			metricsFunc: func(_ context.Context, m Meter) {
				m.Count("test.count", 5, WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.count",
							Value:   Int64MetricValue(5),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Distribution("test.distribution", 3.14, WithUnit("ms"), WithAttributes(attribute.Int("key.int", 42)))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.distribution",
							Value:   Float64MetricValue(3.14),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.int": attribute.Int64Value(42),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Gauge("test.gauge", 2.71, WithUnit("requests"), WithAttributes(attribute.Float64("key.float", 1.618)))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.gauge",
							Value:   Float64MetricValue(2.71),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.float": attribute.Float64Value(1.618),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Count("test.zero.count", 0, WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.count",
							Value:   Int64MetricValue(0),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Distribution("test.zero.distribution", 0, WithUnit("bytes"), WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.distribution",
							Value:   Float64MetricValue(0),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Gauge("test.zero.gauge", 0, WithUnit("connections"), WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.zero.gauge",
							Value:   Float64MetricValue(0),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Count("test.negative.count", -10, WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.count",
							Value:   Int64MetricValue(-10),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Distribution("test.negative.distribution", -2.5, WithUnit("ms"), WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.distribution",
							Value:   Float64MetricValue(-2.5),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			metricsFunc: func(_ context.Context, m Meter) {
				m.Gauge("test.negative.gauge", -5, WithUnit("connections"), WithAttributes(attribute.String("key.string", "value")))
			},
			wantEvents: []Event{
				{
					Metrics: []Metric{
						{
							TraceID: TraceIDFromHex(LogTraceID),
							Name:    "test.negative.gauge",
							Value:   Float64MetricValue(-5),
							Attributes: testutils.MergeMaps(attrs, map[string]attribute.Value{
								"key.string": attribute.StringValue("value"),
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
			ctx, mockTransport := setupMetricsTest()
			meter := NewMeter(ctx)

			tt.metricsFunc(ctx, meter)
			flushFromContext(ctx, testutils.FlushTimeout())

			opts := cmp.Options{
				cmpopts.IgnoreFields(Metric{}, "Timestamp"),
				cmpopts.IgnoreUnexported(MetricValue{}),
				cmp.AllowUnexported(attribute.Value{}),
			}

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
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)
	meter.Count("test.count", 42)
	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
}

func Test_batchMeter_FlushWithContext(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)
	meter.Count("test.count", 42)

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
		EnableTracing: true,
		BeforeSendMetric: func(_ *Metric) *Metric {
			return nil
		},
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub().Clone()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	ctx = SetHubOnContext(ctx, hub)

	meter := NewMeter(ctx)
	meter.Count("test.count", 1)
	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func Test_Meter_ExceedBatchSize(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)
	for i := 0; i < batchSize; i++ {
		meter.Count("test.count", 1)
	}

	// sleep to wait for the batch to be processed
	time.Sleep(time.Millisecond * 20)
	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected only one event with 100 metrics, got %d", len(events))
	}
}

func Test_batchMeter_FlushMultipleTimes(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)

	for i := 0; i < 5; i++ {
		meter.Count("test.count", 1)
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
		meter.Count("test.count", 1)
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
		DisableTelemetryBuffer: true,
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx := SetHubOnContext(context.Background(), hub)
	meter := NewMeter(ctx)
	for i := 0; i < 3; i++ {
		meter.Count("test.count", 1)
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
	ctx, mockTransport := setupMetricsTest()

	// Start a new transaction
	txn := StartTransaction(ctx, "test-transaction")
	defer txn.Finish()

	expectedTraceID := txn.TraceID
	expectedSpanID := txn.SpanID

	meter := NewMeter(ctx)
	meter.WithCtx(txn.Context()).Count("test.count", 42)

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
	meter.Count("test.count", 1)
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
	} else if val.AsString() != "user123" {
		t.Errorf("unexpected user.id: got %v, want %v", val.AsString(), "user123")
	}

	if val, ok := attrs["user.name"]; !ok {
		t.Error("missing user.name attribute")
	} else if val.AsString() != "Test User" {
		t.Errorf("unexpected user.name: got %v, want %v", val.AsString(), "Test User")
	}

	if val, ok := attrs["user.email"]; !ok {
		t.Error("missing user.email attribute")
	} else if val.AsString() != "test@example.com" {
		t.Errorf("unexpected user.email: got %v, want %v", val.AsString(), "test@example.com")
	}
}

func Test_sentryMeter_SetAttributes(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)
	meter.SetAttributes(
		attribute.String("key.string", "some str"),
		attribute.Int("key.int", 42),
		attribute.Int64("key.int64", 17),
		attribute.Float64("key.float", 42.2),
		attribute.Bool("key.bool", true),
	)
	meter.Count("test.count", 1)

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

	assertEqual(t, attrs["key.string"].AsString(), "some str")
	assertEqual(t, attrs["key.int"].AsInt64(), int64(42))
	assertEqual(t, attrs["key.int64"].AsInt64(), int64(17))
	assertEqual(t, attrs["key.float"].AsFloat64(), 42.2)
	assertEqual(t, attrs["key.bool"].AsBool(), true)
}

func Test_sentryMeter_SetAttributes_Persistence(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)
	meter.SetAttributes(attribute.Int("int", 42))
	meter.Count("test.count1", 1)

	meter.SetAttributes(attribute.String("string", "some str"))
	meter.Count("test.count2", 2)
	flushFromContext(ctx, testutils.FlushTimeout())

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Metrics[0].Attributes["int"].AsInt64(), int64(42))
	if _, ok := event.Metrics[1].Attributes["int"]; !ok {
		t.Fatalf("expected key to exist")
	}
	assertEqual(t, event.Metrics[1].Attributes["string"].AsString(), "some str")
}

func Test_sentryMeter_AttributePrecedence(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	hub := GetHubFromContext(ctx)

	hub.Scope().SetUser(User{ID: "user123", Name: "TestUser"})

	meter := NewMeter(ctx)
	meter.SetAttributes(attribute.String("key", "instance-value"))
	meter.SetAttributes(attribute.String("instance-only", "instance"))

	meter.Count("test.precedence", 1,
		WithAttributes(
			attribute.String("key", "call-value"), // Should override instance-level
			attribute.String("call-only", "call"),
		),
	)

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	metrics := events[0].Metrics
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}

	attrs := metrics[0].Attributes

	if val, ok := attrs["key"]; !ok {
		t.Error("missing key attribute")
	} else if val.AsString() != "call-value" {
		t.Errorf("expected key=call-value, got %v", val.AsString())
	}

	if val, ok := attrs["instance-only"]; !ok {
		t.Error("missing instance-only attribute")
	} else if val.AsString() != "instance" {
		t.Errorf("expected instance-only=instance, got %v", val.AsString())
	}

	if val, ok := attrs["call-only"]; !ok {
		t.Error("missing call-only attribute")
	} else if val.AsString() != "call" {
		t.Errorf("expected call-only=call, got %v", val.AsString())
	}

	if val, ok := attrs["user.id"]; !ok {
		t.Error("missing user.id attribute from scope")
	} else if val.AsString() != "user123" {
		t.Errorf("expected user.id=user123, got %v", val.AsString())
	}

	if _, ok := attrs["sentry.sdk.name"]; !ok {
		t.Error("missing sentry.sdk.name default attribute")
	}
}

func TestNewMeter_DisabledMetrics(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:            testDsn,
		Transport:      mockTransport,
		DisableMetrics: true,
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx = SetHubOnContext(ctx, hub)

	meter := NewMeter(ctx)
	meter.Count("test.count", 1)
	meter.Gauge("test.gauge", 2.5)
	meter.Distribution("test.dist", 3.14)

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events with disabled metrics, got %d", len(events))
	}
}

func Test_sentryMeter_EmptyName(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)

	meter.Count("", 1)
	meter.Gauge("", 2.5)
	meter.Distribution("", 3.14)

	flushFromContext(ctx, testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events with empty name, got %d", len(events))
	}
}

func Test_noopMeter_Methods(t *testing.T) {
	hub := NewHub(nil, NewScope())
	ctx := SetHubOnContext(context.Background(), hub)
	meter := NewMeter(ctx)

	meter = meter.WithCtx(ctx)
	meter.Count("test.count", 1)
	meter.Gauge("test.gauge", 2.5)
	meter.Distribution("test.dist", 3.14)
	meter.SetAttributes(attribute.String("key", "value"))

	if _, ok := meter.(*noopMeter); !ok {
		t.Errorf("expected *noopMeter, got %T", meter)
	}
}

func Test_sentryMeter_WithScopeOverride(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()
	meter := NewMeter(ctx)

	customScope := NewScope()
	customScope.SetUser(User{
		ID:    "custom-user-123",
		Email: "custom@example.com",
	})

	meter.Count("test.count.with.scope", 42, WithScopeOverride(customScope))
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
		t.Error("missing user.id attribute from custom scope")
	} else if val.AsString() != "custom-user-123" {
		t.Errorf("unexpected user.id: got %v, want custom-user-123", val.AsString())
	}

	if val, ok := attrs["user.email"]; !ok {
		t.Error("missing user.email attribute from custom scope")
	} else if val.AsString() != "custom@example.com" {
		t.Errorf("unexpected user.email: got %v, want custom@example.com", val.AsString())
	}
}

func TestSentryMeter_ScopeSetAttributesNoLeak(t *testing.T) {
	ctx, mockTransport := setupMetricsTest()

	clonedHub := GetHubFromContext(ctx).Clone()
	clonedHub.Scope().SetAttributes(
		attribute.String("key.string", "str"),
		attribute.Bool("key.bool", true),
	)
	scopedCtx := SetHubOnContext(ctx, clonedHub)
	txn := StartTransaction(scopedCtx, "test-transaction")
	defer txn.Finish()

	meterOutsideScope := NewMeter(ctx)
	meterOutsideScope.Count("test.count.before", 1)
	flushFromContext(ctx, testutils.FlushTimeout())

	eventsBeforeScope := mockTransport.Events()
	if len(eventsBeforeScope) != 1 {
		t.Fatalf("expected 1 event before scope attrs, got %d", len(eventsBeforeScope))
	}
	metricsBeforeScope := eventsBeforeScope[0].Metrics
	if len(metricsBeforeScope) != 1 {
		t.Fatalf("expected 1 metric before scope attrs, got %d", len(metricsBeforeScope))
	}
	assert.NotContains(t, metricsBeforeScope[0].Attributes, "key.bool")
	assert.NotContains(t, metricsBeforeScope[0].Attributes, "key.string")

	mockTransport.events = nil

	meter := NewMeter(txn.Context())
	meter.Count("test.count.after", 2)
	flushFromContext(ctx, testutils.FlushTimeout())

	eventsAfterScope := mockTransport.Events()
	if len(eventsAfterScope) != 1 {
		t.Fatalf("expected 1 event after scope attrs, got %d", len(eventsAfterScope))
	}
	metricsAfterScope := eventsAfterScope[0].Metrics
	if len(metricsAfterScope) != 1 {
		t.Fatalf("expected 1 metric after scope attrs, got %d", len(metricsAfterScope))
	}
	assert.Equal(t, attribute.BoolValue(true), metricsAfterScope[0].Attributes["key.bool"])
	assert.Equal(t, attribute.StringValue("str"), metricsAfterScope[0].Attributes["key.string"])
}
