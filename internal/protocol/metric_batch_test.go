package protocol

import (
	"encoding/json"
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type dummyMetric struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func (d dummyMetric) ToEnvelopeItem() (*EnvelopeItem, error) {
	payload, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}

	return &EnvelopeItem{
		Header:  &EnvelopeItemHeader{Type: EnvelopeItemTypeTraceMetric},
		Payload: payload,
	}, nil
}

func (d dummyMetric) GetCategory() ratelimit.Category              { return ratelimit.CategoryTraceMetric }
func (d dummyMetric) GetEventID() string                           { return "" }
func (d dummyMetric) GetSdkInfo() *SdkInfo                         { return nil }
func (d dummyMetric) GetDynamicSamplingContext() map[string]string { return nil }

func TestMetric_ToEnvelopeItem(t *testing.T) {
	metrics := Metrics{dummyMetric{Name: "metric1", Type: "gauge", Value: 42}, dummyMetric{Name: "metric2", Type: "count", Value: 7}}
	item, err := metrics.ToEnvelopeItem()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item == nil || item.Header == nil || item.Header.Type != EnvelopeItemTypeTraceMetric {
		t.Fatalf("unexpected envelope item: %#v", item)
	}

	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}

	if Metrics(nil).GetCategory() != ratelimit.CategoryTraceMetric {
		t.Fatal("category mismatch")
	}
	if Metrics(nil).GetEventID() != "" {
		t.Fatal("event id should be empty")
	}
	if Metrics(nil).GetSdkInfo() != nil {
		t.Fatal("sdk info should be nil")
	}
	if Metrics(nil).GetDynamicSamplingContext() != nil {
		t.Fatal("dsc should be nil")
	}
}
