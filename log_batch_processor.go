package sentry

import (
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
)

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
	if !p.BatchProcessor.Send(*log) {
		debuglog.Printf("Dropping log [%s]: buffer full", log.Level)
	}
}
