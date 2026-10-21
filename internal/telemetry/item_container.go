package telemetry

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
)

var errNoSerializableItems = errors.New("item container contains no serializable items")

type ItemContainer struct {
	items    []Item
	category ratelimit.Category
}

// NewItemContainer constructs a batched envelope producer from buffered telemetry items.
func NewItemContainer(category ratelimit.Category, items []Item) ItemContainer {
	return ItemContainer{category: category, items: items}
}

func (b ItemContainer) marshalPayload() ([]byte, int, error) {
	items := make([]json.RawMessage, 0, len(b.items))
	for _, item := range b.items {
		itemPayload, err := json.Marshal(item)
		if err != nil {
			continue
		}
		items = append(items, itemPayload)
	}

	if len(items) == 0 {
		return nil, 0, nil
	}

	wrapper := struct {
		Items []json.RawMessage `json:"items"`
	}{Items: items}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		return nil, 0, err
	}
	return payload, len(items), nil
}

func (b ItemContainer) newEnvelopeItem(itemCount int, payload []byte) (*protocol.EnvelopeItem, error) {
	switch b.category {
	case ratelimit.CategoryLog:
		return protocol.NewLogItem(itemCount, payload), nil
	case ratelimit.CategoryTraceMetric:
		return protocol.NewTraceMetricItem(itemCount, payload), nil
	default:
		return nil, fmt.Errorf("unsupported batched category: %s", b.category)
	}
}

func (b ItemContainer) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	payload, itemCount, err := b.marshalPayload()
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, errNoSerializableItems
	}

	return b.newEnvelopeItem(itemCount, payload)
}

func (b ItemContainer) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	item, err := b.ToEnvelopeItem()
	if err != nil {
		return nil, err
	}
	return protocol.NewEnvelope(header, item), nil
}

func (b ItemContainer) GetCategory() ratelimit.Category            { return b.category }
func (ItemContainer) GetEventID() string                           { return "" }
func (ItemContainer) GetSdkInfo() *protocol.SdkInfo                { return nil }
func (ItemContainer) GetDynamicSamplingContext() map[string]string { return nil }
