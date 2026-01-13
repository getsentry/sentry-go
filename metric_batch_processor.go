package sentry

import "time"

// MetricBatchProcessor batches metrics and sends them to Sentry.
type MetricBatchProcessor struct {
	*BatchProcessor[Metric]
}

func NewMetricBatchProcessor(client *Client) *MetricBatchProcessor {
	return &MetricBatchProcessor{
		BatchProcessor: NewBatchProcessor(func(items []Metric) {
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

func (p *MetricBatchProcessor) Send(metric *Metric) {
	p.BatchProcessor.Send(*metric)
}
