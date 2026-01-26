package sentry

import (
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

// metricBatchProcessor batches metrics and sends them to Sentry.
type metricBatchProcessor struct {
	*batchProcessor[Metric]
}

func newMetricBatchProcessor(client *Client) *metricBatchProcessor {
	return &metricBatchProcessor{
		batchProcessor: newBatchProcessor(func(items []Metric) {
			if len(items) == 0 {
				return
			}

			event := NewEvent()
			event.Timestamp = time.Now()
			event.EventID = EventID(uuid())
			event.Type = traceMetricEvent.Type
			event.Metrics = items

			client.Transport.SendEvent(event)
		}),
	}
}

func (p *metricBatchProcessor) Send(metric *Metric) {
	if !p.batchProcessor.Send(*metric) {
		debuglog.Printf("Dropping metric %q: buffer full", metric.Name)
	}
}
