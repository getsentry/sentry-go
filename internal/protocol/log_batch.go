package protocol

import (
	"encoding/json"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

// LogAttribute is the JSON representation for a single log attribute value.
type LogAttribute struct {
	Value any    `json:"value"`
	Type  string `json:"type"`
}

// LogPayload represents the serialized shape of a single log record inside a batched
// log envelope item. Keep in sync with sentry.Log fields that are meant to be emitted.
type LogPayload struct {
	Timestamp  time.Time               `json:"timestamp,omitempty"`
	TraceID    string                  `json:"trace_id,omitempty"`
	Level      string                  `json:"level"`
	Severity   int                     `json:"severity_number,omitempty"`
	Body       string                  `json:"body,omitempty"`
	Attributes map[string]LogAttribute `json:"attributes,omitempty"`
}

// LogPayloader is implemented by items that can convert to a LogPayload for batching.
type LogPayloader interface {
	ToLogPayload() LogPayload
}

// MarshalJSON encodes timestamp as seconds since epoch per Sentry logs spec.
func (lp LogPayload) MarshalJSON() ([]byte, error) {
	// Convert time.Time to seconds float if set
	var ts *float64
	if !lp.Timestamp.IsZero() {
		sec := float64(lp.Timestamp.UnixNano()) / 1e9
		ts = &sec
	}

	out := struct {
		Timestamp  *float64                `json:"timestamp,omitempty"`
		TraceID    string                  `json:"trace_id,omitempty"`
		Level      string                  `json:"level"`
		Severity   int                     `json:"severity_number,omitempty"`
		Body       string                  `json:"body,omitempty"`
		Attributes map[string]LogAttribute `json:"attributes,omitempty"`
	}{
		Timestamp:  ts,
		TraceID:    lp.TraceID,
		Level:      lp.Level,
		Severity:   lp.Severity,
		Body:       lp.Body,
		Attributes: lp.Attributes,
	}
	return json.Marshal(out)
}

// Logs is a container for multiple LogPayload items which knows how to convert
// itself into a single batched log envelope item.
type Logs []LogPayload

func (ls Logs) ToEnvelopeItem() (*EnvelopeItem, error) {
	wrapper := struct {
		Items []LogPayload `json:"items"`
	}{Items: ls}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		return nil, err
	}
	return NewLogItem(len(ls), payload), nil
}

func (Logs) GetCategory() ratelimit.Category              { return ratelimit.CategoryLog }
func (Logs) GetEventID() string                           { return "" }
func (Logs) GetSdkInfo() *SdkInfo                         { return nil }
func (Logs) GetDynamicSamplingContext() map[string]string { return nil }
