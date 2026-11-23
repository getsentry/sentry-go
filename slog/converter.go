package sentryslog

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

var (
	sourceKey = "source"
	errorKeys = map[string]struct{}{
		"error": {},
		"err":   {},
	}
)

// uint64LogEntry is used to pass uint64 values without conversion.
// The concrete sentry.logEntry type satisfies this interface,
// but it is intentionally not part of the public sentry.LogEntry API.
type uint64LogEntry interface {
	Uint64(key string, value uint64) sentry.LogEntry
}

func slogAttrToLogEntry(logEntry sentry.LogEntry, group string, a slog.Attr) sentry.LogEntry {
	key := group + a.Key
	val := a.Value.Resolve()
	switch val.Kind() {
	case slog.KindAny:
		return logEntry.String(key, fmt.Sprintf("%+v", val.Any()))
	case slog.KindBool:
		return logEntry.Bool(key, val.Bool())
	case slog.KindDuration:
		return logEntry.String(key, val.Duration().String())
	case slog.KindFloat64:
		return logEntry.Float64(key, val.Float64())
	case slog.KindInt64:
		return logEntry.Int64(key, val.Int64())
	case slog.KindString:
		return logEntry.String(key, val.String())
	case slog.KindTime:
		return logEntry.String(key, val.Time().Format(time.RFC3339))
	case slog.KindUint64:
		if e, ok := logEntry.(uint64LogEntry); ok {
			return e.Uint64(key, val.Uint64())
		}
		debuglog.Println("Internal error: log entry does not implement unsigned int conversion")
		return logEntry
	case slog.KindGroup:
		// Handle nested group attributes
		groupPrefix := key
		if groupPrefix != "" {
			groupPrefix += "."
		}
		for _, subAttr := range val.Group() {
			logEntry = slogAttrToLogEntry(logEntry, groupPrefix, subAttr)
		}
		return logEntry
	}

	debuglog.Printf("Invalid type: dropping attribute with key: %v and value: %v", a.Key, val)
	return logEntry
}
