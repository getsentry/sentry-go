package sentryslog

import (
	"log/slog"

	"github.com/getsentry/sentry-go"
)

var (
	sourceKey  = "source"
	contextKey = "extra"
	errorKeys  = map[string]struct{}{
		"error": {},
		"err":   {},
	}
	name = "slog"
)

type Converter func(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record, hub *sentry.Hub) *sentry.Event

func DefaultConverter(addSource bool, replaceAttr func(groups []string, a slog.Attr) slog.Attr, loggerAttr []slog.Attr, groups []string, record *slog.Record, hub *sentry.Hub) *sentry.Event {
	// aggregate all attributes
	attrs := appendRecordAttrsToAttrs(loggerAttr, groups, record)

	// developer formatters
	if addSource {
		attrs = append(attrs, source(sourceKey, record))
	}
	attrs = replaceAttrs(replaceAttr, []string{}, attrs...)
	attrs = removeEmptyAttrs(attrs)
	attrs, err := extractError(attrs)

	// handler formatter
	event := sentry.NewEvent()
	event.Timestamp = record.Time.UTC()
	event.Level = LogLevels[record.Level]
	event.Message = record.Message
	event.Logger = name
	event.SetException(err, 10)

	for i := range attrs {
		attrToSentryEvent(attrs[i], event)
	}

	return event
}
