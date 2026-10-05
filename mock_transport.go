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
	mu        sync.Mutex
	events    []*Event
	lastEvent *Event
}

func (t *MockTransport) Configure(_ ClientOptions) {}

// SendEnvelope captures an envelope and decodes its events for assertions.
func (t *MockTransport) SendEnvelope(envelope *protocol.Envelope) error {
	if !validEnvelope(envelope) {
		return ErrInvalidEnvelope
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range envelope.Items {
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
func (t *MockTransport) Flush(_ time.Duration) bool {
	return true
}
func (t *MockTransport) FlushWithContext(_ context.Context) bool { return true }

// Events returns captured telemetry decoded from the wire format. Logs and
// metrics are represented by one Event per batch; client reports are omitted.
// Values in context and extra maps have JSON types, not their original Go types.
func (t *MockTransport) Events() []*Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.events)
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
	event := &Event{}
	switch item.Header.Type {
	case protocol.EnvelopeItemTypeEvent, protocol.EnvelopeItemTypeTransaction:
		payload := mockDecode[struct {
			Event
			Spans []struct {
				Span
				TraceID      string `json:"trace_id"`
				SpanID       string `json:"span_id"`
				ParentSpanID string `json:"parent_span_id"`
				Status       string `json:"status"`
			} `json:"spans"`
		}](item.Payload)
		event = &payload.Event
		for i := range payload.Spans {
			span := &payload.Spans[i]
			decodeMockID(span.TraceID, span.Span.TraceID[:])
			decodeMockID(span.SpanID, span.Span.SpanID[:])
			decodeMockID(span.ParentSpanID, span.Span.ParentSpanID[:])
			for status, name := range spanStatuses {
				if name == span.Status {
					span.Span.Status = SpanStatus(status)
					break
				}
			}
			event.Spans = append(event.Spans, &span.Span)
		}
	case protocol.EnvelopeItemTypeCheckIn:
		payload := mockDecode[struct {
			serializedCheckIn
			MonitorConfig json.RawMessage `json:"monitor_config"`
		}](item.Payload)
		event.Type, event.Release, event.Environment = checkInType, payload.Release, payload.Environment
		event.CheckIn = &CheckIn{ID: EventID(payload.CheckInID), MonitorSlug: payload.MonitorSlug, Status: payload.Status, Duration: time.Duration(payload.Duration * float64(time.Second))}
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
			event.MonitorConfig = &config.MonitorConfig
		}
	case protocol.EnvelopeItemTypeLog, protocol.EnvelopeItemTypeTraceMetric:
		payload := mockDecode[struct {
			Items []json.RawMessage `json:"items"`
		}](item.Payload)
		event.Type = string(item.Header.Type)
		for _, raw := range payload.Items {
			if item.Header.Type == protocol.EnvelopeItemTypeLog {
				log := mockDecode[struct {
					Log
					TraceID    string                     `json:"trace_id"`
					SpanID     string                     `json:"span_id"`
					Attributes map[string]json.RawMessage `json:"attributes"`
				}](raw)
				decodeMockID(log.TraceID, log.Log.TraceID[:])
				decodeMockID(log.SpanID, log.Log.SpanID[:])
				log.Log.Attributes = decodeMockAttributes(log.Attributes)
				log.approximateSize = computeLogSize(&log.Log)
				event.Logs = append(event.Logs, log.Log)
			} else {
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
				event.Metrics = append(event.Metrics, metric.Metric)
			}
		}
	default:
		return nil
	}
	return event
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
