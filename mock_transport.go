package sentry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/protocol"
)

// MockTransport implements [Transport] for use in tests.
type MockTransport struct {
	mu           sync.Mutex
	envelopes    []*protocol.Envelope
	events       []*Event
	lastEvent    *Event
	transactions []*Transaction
	checkIns     []*MockCheckIn
	logs         []MockLogBatch
	metrics      []MockMetricBatch
}

// MockCheckIn is a check-in captured by [MockTransport].
type MockCheckIn struct {
	CheckIn       CheckIn
	MonitorConfig *MonitorConfig
	Release       string
	Environment   string
	TraceID       TraceID
}

// MockLogBatch is a log envelope item captured by [MockTransport].
type MockLogBatch struct {
	Logs []Log
}

// MockMetricBatch is a metric envelope item captured by [MockTransport].
type MockMetricBatch struct {
	Metrics []Metric
}

// Configure marks delivery as synchronous so captured events are visible immediately.
func (t *MockTransport) Configure(options ClientOptions) { options.markSyncDelivery() }

// SendEnvelope captures an envelope and decodes its events for assertions.
func (t *MockTransport) SendEnvelope(_ context.Context, envelope *protocol.Envelope) error {
	if !validEnvelope(envelope) {
		return ErrInvalidEnvelope
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.envelopes = append(t.envelopes, envelope)
	for _, item := range envelope.Items {
		switch item.Header.Type {
		case protocol.EnvelopeItemTypeTransaction:
			t.transactions = append(t.transactions, decodeMockTransaction(item))
			continue
		case protocol.EnvelopeItemTypeCheckIn:
			t.checkIns = append(t.checkIns, decodeMockCheckIn(item))
			continue
		case protocol.EnvelopeItemTypeLog:
			t.logs = append(t.logs, MockLogBatch{Logs: decodeMockLogs(item)})
			continue
		case protocol.EnvelopeItemTypeTraceMetric:
			t.metrics = append(t.metrics, MockMetricBatch{Metrics: decodeMockMetrics(item)})
			continue
		}
		event := decodeMockEvent(item)
		if event == nil {
			continue
		}
		if event.EventID == "" {
			event.EventID = EventID(envelope.Header.EventID)
		}
		if event.Sdk.Name == "" && envelope.Header.Sdk != nil {
			event.Sdk = *envelope.Header.Sdk
		}
		if len(envelope.Header.Trace) > 0 {
			event.sdkMetaData.dsc.Entries = maps.Clone(envelope.Header.Trace)
		}
		for _, attachment := range envelope.Items {
			if attachment.Header.Type == protocol.EnvelopeItemTypeAttachment {
				event.Attachments = append(event.Attachments, &Attachment{
					Filename: attachment.Header.Filename, ContentType: attachment.Header.ContentType, Payload: attachment.Payload,
				})
			}
		}
		t.events = append(t.events, event)
		t.lastEvent = event
	}
	return nil
}
func (t *MockTransport) FlushWithContext(_ context.Context) bool { return true }

// Envelopes returns the captured envelopes.
func (t *MockTransport) Envelopes() []*protocol.Envelope {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.envelopes)
}

// Events returns captured events decoded from the wire format.
// Values in context and extra maps have JSON types, not their original Go types.
func (t *MockTransport) Events() []*Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.events)
}

// Transactions returns captured transactions decoded from the wire format.
// The trace context is decoded into Trace. Values in context and data maps
// have JSON types, not their original Go types.
func (t *MockTransport) Transactions() []*Transaction {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.transactions)
}

// LogBatches returns the logs of each captured log envelope item.
func (t *MockTransport) LogBatches() []MockLogBatch {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.logs)
}

// MetricBatches returns the metrics of each captured metric envelope item.
func (t *MockTransport) MetricBatches() []MockMetricBatch {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.metrics)
}

// CheckIns returns captured check-ins decoded from the wire format.
func (t *MockTransport) CheckIns() []*MockCheckIn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.checkIns)
}

func (t *MockTransport) Close() {}

// mockDecode panics on invalid payloads so tests cannot silently ignore them.
func mockDecode[T any](raw []byte) T {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		panic(fmt.Errorf("MockTransport: %w", err))
	}
	return value
}

func decodeMockEvent(item *protocol.EnvelopeItem) *Event {
	if item.Header.Type != protocol.EnvelopeItemTypeEvent {
		return nil
	}
	return mockDecode[*Event](item.Payload)
}

func decodeMockTransaction(item *protocol.EnvelopeItem) *Transaction {
	payload := mockDecode[struct {
		Transaction
		Spans []mockSpan `json:"spans"`
	}](item.Payload)
	tx := &payload.Transaction
	for i := range payload.Spans {
		tx.Spans = append(tx.Spans, payload.Spans[i].decode())
	}
	if trace, ok := tx.Contexts[traceContextKey]; ok {
		raw, _ := json.Marshal(trace)
		root := mockDecode[*mockSpan](raw).decode()
		tx.Trace = TraceContext{
			TraceID: root.TraceID, SpanID: root.SpanID, ParentSpanID: root.ParentSpanID,
			Op: root.Op, Description: root.Description, Status: root.Status, Data: root.Data, Origin: root.Origin,
		}
		delete(tx.Contexts, traceContextKey)
	}
	return tx
}

// mockSpan decodes a span or a trace context, whose IDs and status are strings on the wire.
type mockSpan struct {
	Span
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	Status       string `json:"status"`
}

func (s *mockSpan) decode() *Span {
	decodeMockID(s.TraceID, s.Span.TraceID[:])
	decodeMockID(s.SpanID, s.Span.SpanID[:])
	decodeMockID(s.ParentSpanID, s.Span.ParentSpanID[:])
	for status, name := range spanStatuses {
		if name == s.Status {
			s.Span.Status = SpanStatus(status)
			break
		}
	}
	return &s.Span
}

func decodeMockLogs(item *protocol.EnvelopeItem) []Log {
	items := mockDecode[struct{ Items []json.RawMessage }](item.Payload).Items
	logs := make([]Log, 0, len(items))
	for _, raw := range items {
		log := mockDecode[struct {
			Log
			TraceID    string                     `json:"trace_id"`
			SpanID     string                     `json:"span_id"`
			Attributes map[string]json.RawMessage `json:"attributes"`
		}](raw)
		decodeMockID(log.TraceID, log.Log.TraceID[:])
		decodeMockID(log.SpanID, log.Log.SpanID[:])
		log.Log.Attributes = decodeMockAttributes(log.Attributes)
		logs = append(logs, log.Log)
	}
	return logs
}

func decodeMockMetrics(item *protocol.EnvelopeItem) []Metric {
	items := mockDecode[struct{ Items []json.RawMessage }](item.Payload).Items
	metrics := make([]Metric, 0, len(items))
	for _, raw := range items {
		metric := mockDecode[struct {
			Metric
			TraceID    string                     `json:"trace_id"`
			SpanID     string                     `json:"span_id"`
			Value      json.RawMessage            `json:"value"`
			Attributes map[string]json.RawMessage `json:"attributes"`
		}](raw)
		decodeMockID(metric.TraceID, metric.Metric.TraceID[:])
		decodeMockID(metric.SpanID, metric.Metric.SpanID[:])
		metric.Metric.Attributes = decodeMockAttributes(metric.Attributes)
		if metric.Type == MetricTypeCounter {
			metric.Metric.Value = Int64MetricValue(mockDecode[int64](metric.Value))
		} else {
			metric.Metric.Value = Float64MetricValue(mockDecode[float64](metric.Value))
		}
		metrics = append(metrics, metric.Metric)
	}
	return metrics
}

func decodeMockCheckIn(item *protocol.EnvelopeItem) *MockCheckIn {
	payload := mockDecode[struct {
		checkInItem
		MonitorConfig json.RawMessage `json:"monitor_config"`
	}](item.Payload)
	checkIn := &MockCheckIn{
		CheckIn: CheckIn{
			ID:          EventID(payload.CheckInID),
			MonitorSlug: payload.MonitorSlug,
			Status:      payload.Status,
			Duration:    time.Duration(payload.Duration * float64(time.Second)),
		},
		Release:     payload.Release,
		Environment: payload.Environment,
	}
	traceID, _ := payload.Contexts[traceContextKey][traceIDContextKey].(string)
	decodeMockID(traceID, checkIn.TraceID[:])
	if len(payload.MonitorConfig) != 0 && string(payload.MonitorConfig) != "null" {
		config := mockDecode[struct {
			MonitorConfig
			Schedule struct {
				Type  string
				Value json.RawMessage
				Unit  MonitorScheduleUnit
			} `json:"schedule"`
		}](payload.MonitorConfig)
		switch config.Schedule.Type {
		case "crontab":
			config.MonitorConfig.Schedule = CrontabSchedule(mockDecode[string](config.Schedule.Value))
		case "interval":
			config.MonitorConfig.Schedule = IntervalSchedule(mockDecode[int64](config.Schedule.Value), config.Schedule.Unit)
		}
		checkIn.MonitorConfig = &config.MonitorConfig
	}
	return checkIn
}

func decodeMockID(value string, dst []byte) {
	if value == "" {
		return
	}
	if len(value) != hex.EncodedLen(len(dst)) {
		panic(fmt.Errorf("MockTransport: invalid ID length %d", len(value)))
	}
	if _, err := hex.Decode(dst, []byte(value)); err != nil {
		panic(fmt.Errorf("MockTransport: %w", err))
	}
}

func decodeMockAttributes(values map[string]json.RawMessage) map[string]attribute.Value {
	if values == nil {
		return nil
	}
	result := make(map[string]attribute.Value, len(values))
	for key, raw := range values {
		value := mockDecode[struct {
			Type  string
			Value json.RawMessage
		}](raw)
		switch value.Type {
		case "boolean":
			result[key] = attribute.BoolValue(mockDecode[bool](value.Value))
		case "integer":
			var signed int64
			if json.Unmarshal(value.Value, &signed) == nil {
				result[key] = attribute.Int64Value(signed)
			} else {
				result[key] = attribute.Uint64Value(mockDecode[uint64](value.Value))
			}
		case "double":
			result[key] = attribute.Float64Value(mockDecode[float64](value.Value))
		case "string":
			result[key] = attribute.StringValue(mockDecode[string](value.Value))
		case "array":
			var strings []string
			var bools []bool
			var ints []int64
			switch {
			case json.Unmarshal(value.Value, &strings) == nil:
				result[key] = attribute.StringSliceValue(strings)
			case json.Unmarshal(value.Value, &bools) == nil:
				result[key] = attribute.BoolSliceValue(bools)
			case json.Unmarshal(value.Value, &ints) == nil:
				result[key] = attribute.Int64SliceValue(ints)
			default:
				result[key] = attribute.Float64SliceValue(mockDecode[[]float64](value.Value))
			}
		default:
			panic(fmt.Errorf("MockTransport: unknown attribute type %q", value.Type))
		}
	}
	return result
}
