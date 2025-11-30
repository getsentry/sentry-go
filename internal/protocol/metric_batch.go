package protocol

import (
	"encoding/json"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type Metrics []EnvelopeItemConvertible

func (ms Metrics) ToEnvelopeItem() (*EnvelopeItem, error) {
	// Convert each metric to its JSON representation
	items := make([][]byte, 0, len(ms))
	for _, metric := range ms {
		envItem, err := metric.ToEnvelopeItem()
		if err != nil {
			continue
		}
		items = append(items, envItem.Payload)
	}

	if len(items) == 0 {
		return nil, nil
	}

	wrapper := struct {
		Items [][]byte `json:"items"`
	}{Items: items}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		return nil, err
	}

	return NewTraceMetricItem(len(items), payload), nil
}

func (Metrics) GetCategory() ratelimit.Category              { return ratelimit.CategoryTraceMetric }
func (Metrics) GetEventID() string                           { return "" }
func (Metrics) GetSdkInfo() *SdkInfo                         { return nil }
func (Metrics) GetDynamicSamplingContext() map[string]string { return nil }
