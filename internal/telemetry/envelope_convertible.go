package telemetry

import (
	"time"

	"github.com/getsentry/sentry-go"
)

// EnvelopeConvertible defines the interface for items that can be converted to Sentry envelopes.
// Each type handles its own batching and envelope creation completely.
type EnvelopeConvertible interface {
	ToEnvelope(dsn *sentry.Dsn, sentAt time.Time) (*sentry.Envelope, error)
	GetCategory() DataCategory
	GetPriority() Priority
	CanBatchWith(other EnvelopeConvertible) bool
	BatchWith(other EnvelopeConvertible) EnvelopeConvertible
}

// EventWrapper wraps a sentry.Event to implement EnvelopeConvertible
type EventWrapper struct {
	Event    *sentry.Event
	Category DataCategory
}

// NewEventWrapper creates a new EventWrapper with the appropriate category
func NewEventWrapper(event *sentry.Event, category DataCategory) *EventWrapper {
	return &EventWrapper{
		Event:    event,
		Category: category,
	}
}

func (e *EventWrapper) ToEnvelope(dsn *sentry.Dsn, sentAt time.Time) (*sentry.Envelope, error) {
	return e.Event.ToEnvelopeWithTime(dsn, sentAt)
}

func (e *EventWrapper) GetCategory() DataCategory {
	return e.Category
}

func (e *EventWrapper) GetPriority() Priority {
	return e.Category.GetPriority()
}

func (e *EventWrapper) CanBatchWith(other EnvelopeConvertible) bool {
	// Events typically don't batch with other events
	return false
}

func (e *EventWrapper) BatchWith(other EnvelopeConvertible) EnvelopeConvertible {
	// Events don't batch, so return self
	return e
}

// LogBatch wraps a batch of sentry.Log items to implement EnvelopeConvertible
type LogBatch struct {
	Logs []sentry.Log
}

// NewLogBatch creates a new LogBatch
func NewLogBatch(logs []sentry.Log) *LogBatch {
	return &LogBatch{
		Logs: logs,
	}
}

func (l *LogBatch) ToEnvelope(dsn *sentry.Dsn, sentAt time.Time) (*sentry.Envelope, error) {
	// Create a dummy event to use the existing log envelope creation logic
	// TODO: logs should have their own envelope transformation, should fix that
	event := sentry.NewEvent()
	event.Type = "log"
	event.Logs = l.Logs

	return event.ToEnvelopeWithTime(dsn, sentAt)
}

func (l *LogBatch) GetCategory() DataCategory {
	return DataCategoryLog
}

func (l *LogBatch) GetPriority() Priority {
	return DataCategoryLog.GetPriority()
}

func (l *LogBatch) CanBatchWith(other EnvelopeConvertible) bool {
	switch other.(type) {
	case *LogBatch, *SingleLog:
		return true
	default:
		return false
	}
}

func (l *LogBatch) BatchWith(other EnvelopeConvertible) EnvelopeConvertible {
	switch otherLog := other.(type) {
	case *LogBatch:
		combinedLogs := make([]sentry.Log, 0, len(l.Logs)+len(otherLog.Logs))
		combinedLogs = append(combinedLogs, l.Logs...)
		combinedLogs = append(combinedLogs, otherLog.Logs...)
		return NewLogBatch(combinedLogs)
	case *SingleLog:
		combinedLogs := make([]sentry.Log, 0, len(l.Logs)+1)
		combinedLogs = append(combinedLogs, l.Logs...)
		combinedLogs = append(combinedLogs, otherLog.Log)
		return NewLogBatch(combinedLogs)
	default:
		return l
	}
}

// SingleLog wraps a single sentry.Log to implement EnvelopeConvertible
type SingleLog struct {
	Log sentry.Log
}

// NewSingleLog creates a new SingleLog wrapper
func NewSingleLog(log sentry.Log) *SingleLog {
	return &SingleLog{
		Log: log,
	}
}

func (s *SingleLog) ToEnvelope(dsn *sentry.Dsn, sentAt time.Time) (*sentry.Envelope, error) {
	batch := NewLogBatch([]sentry.Log{s.Log})
	return batch.ToEnvelope(dsn, sentAt)
}

func (s *SingleLog) GetCategory() DataCategory {
	return DataCategoryLog
}

func (s *SingleLog) GetPriority() Priority {
	return DataCategoryLog.GetPriority()
}

func (s *SingleLog) CanBatchWith(other EnvelopeConvertible) bool {
	switch other.(type) {
	case *LogBatch, *SingleLog:
		return true
	default:
		return false
	}
}

func (s *SingleLog) BatchWith(other EnvelopeConvertible) EnvelopeConvertible {
	switch otherLog := other.(type) {
	case *LogBatch:
		combinedLogs := make([]sentry.Log, 0, len(otherLog.Logs)+1)
		combinedLogs = append(combinedLogs, s.Log)
		combinedLogs = append(combinedLogs, otherLog.Logs...)
		return NewLogBatch(combinedLogs)
	case *SingleLog:
		return NewLogBatch([]sentry.Log{s.Log, otherLog.Log})
	default:
		return s
	}
}

// ConvertibleFactory provides helper methods to create EnvelopeConvertible items
type ConvertibleFactory struct{}

// NewConvertibleFactory creates a new factory
func NewConvertibleFactory() *ConvertibleFactory {
	return &ConvertibleFactory{}
}

// CreateFromEvent creates an EnvelopeConvertible from a sentry.Event
func (f *ConvertibleFactory) CreateFromEvent(event *sentry.Event) EnvelopeConvertible {
	var category DataCategory
	switch event.Type {
	case "transaction":
		category = DataCategoryTransaction
	case "check_in":
		category = DataCategoryCheckIn
	case "log":
		category = DataCategoryLog
	default:
		category = DataCategoryError // Default for error events
	}

	return NewEventWrapper(event, category)
}

// CreateFromLog creates an EnvelopeConvertible from a sentry.Log
func (f *ConvertibleFactory) CreateFromLog(log sentry.Log) EnvelopeConvertible {
	return NewSingleLog(log)
}

// CreateFromLogs creates an EnvelopeConvertible from multiple sentry.Log items
func (f *ConvertibleFactory) CreateFromLogs(logs []sentry.Log) EnvelopeConvertible {
	return NewLogBatch(logs)
}
