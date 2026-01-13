package sentry

import "time"

// LogBatchProcessor batches logs and sends them to Sentry.
type LogBatchProcessor struct {
	*BatchProcessor[Log]
}

func NewLogBatchProcessor(client *Client) *LogBatchProcessor {
	return &LogBatchProcessor{
		BatchProcessor: NewBatchProcessor(func(items []Log) {
			if len(items) == 0 {
				return
			}

			event := NewEvent()
			event.Timestamp = time.Now()
			event.EventID = EventID(uuid())
			event.Type = logEvent.Type
			event.Logs = items

			client.Transport.SendEvent(event)
		}),
	}
}

func (p *LogBatchProcessor) Send(log *Log) {
	p.BatchProcessor.Send(*log)
}
