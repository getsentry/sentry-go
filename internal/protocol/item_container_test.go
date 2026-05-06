package protocol

import (
	"encoding/json"
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type dummyLog struct{ body string }

func (dummyLog) GetCategory() ratelimit.Category { return ratelimit.CategoryLog }
func (dummyLog) MakeSerializationSafe()          {}

type dummyMetric struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func (dummyMetric) GetCategory() ratelimit.Category { return ratelimit.CategoryTraceMetric }
func (dummyMetric) MakeSerializationSafe()          {}

type failingMetric struct {
	Name  string        `json:"name"`
	Value func() string `json:"value"`
}

func (failingMetric) GetCategory() ratelimit.Category { return ratelimit.CategoryTraceMetric }
func (failingMetric) MakeSerializationSafe()          {}

func TestItemContainer_ToEnvelopeItem_And_Getters(t *testing.T) {
	tests := []struct {
		name      string
		category  ratelimit.Category
		items     []TelemetryItem
		itemType  EnvelopeItemType
		wantItems int
	}{
		{
			name:      "logs",
			category:  ratelimit.CategoryLog,
			items:     []TelemetryItem{dummyLog{body: "a"}, dummyLog{body: "b"}},
			itemType:  EnvelopeItemTypeLog,
			wantItems: 2,
		},
		{
			name:     "metrics",
			category: ratelimit.CategoryTraceMetric,
			items: []TelemetryItem{
				dummyMetric{Name: "metric1", Type: "gauge", Value: 42},
				dummyMetric{Name: "metric2", Type: "count", Value: 7},
			},
			itemType:  EnvelopeItemTypeTraceMetric,
			wantItems: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := NewItemContainer(tt.category, tt.items)
			item, err := container.ToEnvelopeItem()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if item == nil || item.Header == nil || item.Header.Type != tt.itemType {
				t.Fatalf("unexpected envelope item: %#v", item)
			}
			if item.Header.ItemCount == nil || *item.Header.ItemCount != tt.wantItems {
				t.Fatalf("expected item_count %d, got %#v", tt.wantItems, item.Header.ItemCount)
			}

			var payload struct {
				Items []json.RawMessage `json:"items"`
			}
			if err := json.Unmarshal(item.Payload, &payload); err != nil {
				t.Fatalf("failed to unmarshal payload: %v", err)
			}
			if len(payload.Items) != tt.wantItems {
				t.Fatalf("expected %d items, got %d", tt.wantItems, len(payload.Items))
			}

			if container.GetCategory() != tt.category {
				t.Fatal("category mismatch")
			}
			if container.GetEventID() != "" {
				t.Fatal("event id should be empty")
			}
			if container.GetSdkInfo() != nil {
				t.Fatal("sdk info should be nil")
			}
			if container.GetDynamicSamplingContext() != nil {
				t.Fatal("dsc should be nil")
			}
		})
	}
}

func TestItemContainer_PartialMarshalFailureUsesSerializedItemCount(t *testing.T) {
	container := NewItemContainer(ratelimit.CategoryTraceMetric, []TelemetryItem{
		dummyMetric{Name: "metric1", Type: "gauge", Value: 42},
		failingMetric{Name: "bad", Value: func() string { return "nope" }},
		dummyMetric{Name: "metric2", Type: "count", Value: 7},
	})

	item, err := container.ToEnvelopeItem()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item == nil || item.Header == nil || item.Header.ItemCount == nil {
		t.Fatalf("unexpected envelope item: %#v", item)
	}
	if got := *item.Header.ItemCount; got != 2 {
		t.Fatalf("expected item_count 2, got %d", got)
	}

	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if got := len(payload.Items); got != 2 {
		t.Fatalf("expected 2 serialized items, got %d", got)
	}
}

func TestItemContainer_AllItemsFailMarshal(t *testing.T) {
	container := NewItemContainer(ratelimit.CategoryTraceMetric, []TelemetryItem{
		failingMetric{Name: "bad1", Value: func() string { return "nope" }},
		failingMetric{Name: "bad2", Value: func() string { return "still nope" }},
	})

	item, err := container.ToEnvelopeItem()
	if err != errNoSerializableItems {
		t.Fatalf("expected errNoSerializableItems, got %v", err)
	}
	if item != nil {
		t.Fatalf("expected nil item, got %#v", item)
	}
}

func TestItemContainer_UnsupportedCategory(t *testing.T) {
	container := NewItemContainer(ratelimit.CategoryError, []TelemetryItem{
		dummyMetric{Name: "metric1", Type: "gauge", Value: 42},
	})
	if _, err := container.ToEnvelopeItem(); err == nil {
		t.Fatal("expected unsupported batched category error")
	}
}
