package sentry

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/protocol"
)

// MockTransport captures envelopes for tests. Its zero value is ready to use.
// Flush the client before inspecting captures; the mock does not flush the
// client's telemetry buffers itself.
type MockTransport struct {
	mu        sync.Mutex
	envelopes []*protocol.Envelope
}

var _ Transport = (*MockTransport)(nil)

// SendEnvelope retains the envelope. As with any transport, the caller must
// not mutate it after handing it over.
func (t *MockTransport) SendEnvelope(envelope *protocol.Envelope) error {
	if envelope == nil || len(envelope.Items) == 0 {
		return ErrEmptyEnvelope
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.envelopes = append(t.envelopes, envelope)
	return nil
}

// HasCapacity always returns true.
func (*MockTransport) HasCapacity() bool { return true }

// IsRateLimited always returns false.
func (*MockTransport) IsRateLimited(protocol.Category) bool { return false }

// Flush returns true because accepted envelopes are stored immediately.
func (*MockTransport) Flush(time.Duration) bool { return true }

// FlushWithContext reports whether the context is still active.
func (*MockTransport) FlushWithContext(ctx context.Context) bool { return ctx.Err() == nil }

// Close does nothing; the mock owns no background resources.
func (*MockTransport) Close() {}

// Envelopes returns a snapshot of the captured envelope list. The envelopes
// themselves are owned by the transport and must be treated as read-only.
func (t *MockTransport) Envelopes() []*protocol.Envelope {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.envelopes)
}

// Reset clears the captured envelopes. Flush the client first to ensure that
// earlier telemetry will not arrive after the reset.
func (t *MockTransport) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.envelopes = nil
}

// Events decodes captured envelopes into an event view for assertions.
// Logs and metrics are represented by one Event per batch. Attachments are
// associated with their event; client reports are available only in Envelopes.
// Context maps contain JSON values, not the original Go types. Invalid payloads
// panic so tests cannot silently overlook malformed telemetry.
func (t *MockTransport) Events() []*Event {
	var events []*Event
	for _, envelope := range t.Envelopes() {
		for _, item := range envelope.Items {
			if item == nil || item.Header == nil {
				continue
			}
			event := decodeMockEvent(item)
			if event == nil {
				continue
			}
			if header := envelope.Header; header != nil {
				if event.EventID == "" {
					event.EventID = EventID(header.EventID)
				}
				if event.Sdk.Name == "" && header.Sdk != nil {
					event.Sdk = *header.Sdk
				}
				if event.Timestamp.IsZero() {
					event.Timestamp = header.SentAt
				}
			}
			if item.Header.Type == protocol.EnvelopeItemTypeEvent || item.Header.Type == protocol.EnvelopeItemTypeTransaction {
				for _, attachment := range envelope.Items {
					if attachment != nil && attachment.Header != nil && attachment.Header.Type == protocol.EnvelopeItemTypeAttachment {
						event.Attachments = append(event.Attachments, &Attachment{
							Filename: attachment.Header.Filename, ContentType: attachment.Header.ContentType,
							Payload: attachment.Payload,
						})
					}
				}
			}
			events = append(events, event)
		}
	}
	return events
}

func decodeMockEvent(item *protocol.EnvelopeItem) *Event {
	check := func(err error) {
		if err != nil {
			panic(fmt.Errorf("MockTransport: decode %s: %w", item.Header.Type, err))
		}
	}
	decode := func(raw []byte, dst any) {
		check(json.Unmarshal(raw, dst))
	}
	event := &Event{}
	switch item.Header.Type {
	case protocol.EnvelopeItemTypeEvent, protocol.EnvelopeItemTypeTransaction:
		payload := struct {
			*Event
			Spans []struct {
				Span
				TraceID      mockTraceID `json:"trace_id"`
				SpanID       mockSpanID  `json:"span_id"`
				ParentSpanID mockSpanID  `json:"parent_span_id"`
				Status       string      `json:"status"`
			} `json:"spans"`
		}{Event: event}
		decode(item.Payload, &payload)
		for i := range payload.Spans {
			span := &payload.Spans[i]
			span.Span.TraceID, span.Span.SpanID, span.Span.ParentSpanID = TraceID(span.TraceID), SpanID(span.SpanID), SpanID(span.ParentSpanID)
			for status, name := range spanStatuses {
				if name == span.Status {
					span.Span.Status = SpanStatus(status)
					break
				}
			}
			event.Spans = append(event.Spans, &span.Span)
		}
	case protocol.EnvelopeItemTypeCheckIn:
		payload := struct {
			serializedCheckIn
			MonitorConfig json.RawMessage `json:"monitor_config"`
		}{}
		decode(item.Payload, &payload)
		event.Type, event.Release, event.Environment = checkInType, payload.Release, payload.Environment
		event.CheckIn = &CheckIn{ID: EventID(payload.CheckInID), MonitorSlug: payload.MonitorSlug, Status: payload.Status, Duration: time.Duration(payload.Duration * float64(time.Second))}
		if len(payload.MonitorConfig) != 0 && string(payload.MonitorConfig) != "null" {
			config := struct {
				*MonitorConfig
				Schedule struct {
					Type  string
					Value json.RawMessage
					Unit  MonitorScheduleUnit
				} `json:"schedule"`
			}{MonitorConfig: &MonitorConfig{}}
			decode(payload.MonitorConfig, &config)
			switch config.Schedule.Type {
			case "crontab":
				var value string
				decode(config.Schedule.Value, &value)
				config.MonitorConfig.Schedule = CrontabSchedule(value)
			case "interval":
				var value int64
				decode(config.Schedule.Value, &value)
				config.MonitorConfig.Schedule = IntervalSchedule(value, config.Schedule.Unit)
			}
			event.MonitorConfig = config.MonitorConfig
		}
	case protocol.EnvelopeItemTypeLog, protocol.EnvelopeItemTypeTraceMetric:
		var payload struct {
			Items []json.RawMessage `json:"items"`
		}
		decode(item.Payload, &payload)
		event.Type = string(item.Header.Type)
		for _, raw := range payload.Items {
			if item.Header.Type == protocol.EnvelopeItemTypeLog {
				log := struct {
					Log
					TraceID    mockTraceID              `json:"trace_id"`
					SpanID     mockSpanID               `json:"span_id"`
					Attributes map[string]mockAttribute `json:"attributes"`
				}{}
				decode(raw, &log)
				log.Log.TraceID, log.Log.SpanID = TraceID(log.TraceID), SpanID(log.SpanID)
				log.Log.Attributes = decodeMockAttributes(log.Attributes)
				log.approximateSize = computeLogSize(&log.Log)
				event.Logs = append(event.Logs, log.Log)
			} else {
				metric := struct {
					Metric
					TraceID    mockTraceID              `json:"trace_id"`
					SpanID     mockSpanID               `json:"span_id"`
					Value      json.Number              `json:"value"`
					Attributes map[string]mockAttribute `json:"attributes"`
				}{}
				decode(raw, &metric)
				metric.Metric.TraceID, metric.Metric.SpanID = TraceID(metric.TraceID), SpanID(metric.SpanID)
				metric.Metric.Attributes = decodeMockAttributes(metric.Attributes)
				if metric.Type == MetricTypeCounter {
					value, err := metric.Value.Int64()
					check(err)
					metric.Metric.Value = Int64MetricValue(value)
				} else {
					value, err := metric.Value.Float64()
					check(err)
					metric.Metric.Value = Float64MetricValue(value)
				}
				event.Metrics = append(event.Metrics, metric.Metric)
			}
		}
	default:
		return nil
	}
	return event
}

type mockTraceID TraceID
type mockSpanID SpanID

func decodeMockID(raw []byte, dst []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if len(value) != 2*len(dst) {
		return fmt.Errorf("invalid ID length: %d", len(value))
	}
	_, err := hex.Decode(dst, []byte(value))
	return err
}

func (id *mockTraceID) UnmarshalJSON(raw []byte) error { return decodeMockID(raw, id[:]) }
func (id *mockSpanID) UnmarshalJSON(raw []byte) error  { return decodeMockID(raw, id[:]) }

type mockAttribute struct{ value attribute.Value }

func (a *mockAttribute) UnmarshalJSON(raw []byte) error {
	var payload struct {
		Type  string
		Value json.RawMessage
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	value := string(payload.Value)
	switch payload.Type {
	case "boolean":
		v, err := strconv.ParseBool(value)
		a.value = attribute.BoolValue(v)
		return err
	case "integer":
		if v, err := strconv.ParseInt(value, 10, 64); err == nil {
			a.value = attribute.Int64Value(v)
			return nil
		}
		v, err := strconv.ParseUint(value, 10, 64)
		a.value = attribute.Uint64Value(v)
		return err
	case "double":
		v, err := strconv.ParseFloat(value, 64)
		a.value = attribute.Float64Value(v)
		return err
	case "string":
		var v string
		err := json.Unmarshal(payload.Value, &v)
		a.value = attribute.StringValue(v)
		return err
	case "array":
		var values []json.RawMessage
		if err := json.Unmarshal(payload.Value, &values); err != nil {
			return err
		}
		if len(values) == 0 {
			a.value = attribute.StringSliceValue(nil)
			return nil
		}
		first := string(values[0])
		switch {
		case strings.HasPrefix(first, "\""):
			var v []string
			err := json.Unmarshal(payload.Value, &v)
			a.value = attribute.StringSliceValue(v)
			return err
		case first == "true" || first == "false":
			var v []bool
			err := json.Unmarshal(payload.Value, &v)
			a.value = attribute.BoolSliceValue(v)
			return err
		default:
			var integers []int64
			if err := json.Unmarshal(payload.Value, &integers); err == nil {
				a.value = attribute.Int64SliceValue(integers)
				return nil
			}
			var v []float64
			err := json.Unmarshal(payload.Value, &v)
			a.value = attribute.Float64SliceValue(v)
			return err
		}
	default:
		return fmt.Errorf("unknown attribute type %q", payload.Type)
	}
}

func decodeMockAttributes(values map[string]mockAttribute) map[string]attribute.Value {
	if values == nil {
		return nil
	}
	result := make(map[string]attribute.Value, len(values))
	for key, value := range values {
		result[key] = value.value
	}
	return result
}
