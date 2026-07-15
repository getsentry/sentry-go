// Package sentrylogrus provides a simple Logrus hook for Sentry.
package sentrylogrus

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/sirupsen/logrus"
)

const (
	// sdkIdentifier is the identifier of the Logrus SDK.
	sdkIdentifier = "sentry.go.logrus"

	// These fields are simply omitted, as they are duplicated by the Sentry SDK.
	FieldGoVersion = "go_version"
	FieldMaxProcs  = "go_maxprocs"

	LogrusOrigin = "auto.log.logrus"
)

// Hook is the logrus hook for Sentry.
//
// It is not safe to configure the hook while logging is happening. Please
// perform all configuration before using it.
type Hook interface {
	// SetHubProvider sets a function to provide a hub for each log entry.
	SetHubProvider(provider func() *sentry.Hub)
	// AddTags adds tags to the hook's scope.
	AddTags(tags map[string]string)
	// SetFallback sets a fallback function for the eventHook.
	SetFallback(fb FallbackFunc)
	// SetKey sets an alternate field key for the eventHook.
	SetKey(oldKey, newKey string)
	// Levels returns the list of logging levels that will be sent to Sentry as events.
	Levels() []logrus.Level
	// Fire sends entry to Sentry as an event.
	Fire(entry *logrus.Entry) error
	// Flush waits until the underlying Sentry transport sends any buffered events.
	Flush(timeout time.Duration) bool
	// FlushWithContext waits for the underlying Sentry transport to send any buffered
	// events, blocking until the context's deadline is reached or the context is canceled.
	// It returns false if the context is canceled or its deadline expires before the events
	// are sent, meaning some events may not have been sent.
	FlushWithContext(ctx context.Context) bool
}

// A FallbackFunc can be used to attempt to handle any errors in logging, before
// resorting to Logrus's standard error reporting.
type FallbackFunc func(*logrus.Entry) error

type logHook struct {
	defaultHub        *sentry.Hub
	hubProvider       func() *sentry.Hub
	useCustomProvider bool
	fallback          FallbackFunc
	keys              map[string]string
	levels            []logrus.Level
	attributes        []attribute.Builder
	logger            sentry.Logger
}

var _ Hook = &logHook{}
var _ logrus.Hook = &logHook{} // logHook also needs to be a logrus.Hook

func (h *logHook) SetHubProvider(provider func() *sentry.Hub) {
	h.hubProvider = provider
	h.useCustomProvider = true
}

func (h *logHook) AddTags(tags map[string]string) {
	// for logs convert tags to attributes
	for k, v := range tags {
		attr := attribute.String(k, v)
		h.attributes = append(h.attributes, attr)
		h.logger.SetAttributes(attr)
	}
}

func (h *logHook) SetFallback(fb FallbackFunc) {
	h.fallback = fb
}

func (h *logHook) SetKey(oldKey, newKey string) {
	if oldKey == "" {
		return
	}
	if newKey == "" {
		delete(h.keys, oldKey)
		return
	}
	delete(h.keys, newKey)
	h.keys[oldKey] = newKey
}

func (h *logHook) key(key string) string {
	if val := h.keys[key]; val != "" {
		return val
	}
	return key
}

// uint64LogEntry is used to pass uint64 values without conversion.
// The concrete sentry.logEntry type satisfies this interface,
// but it is intentionally not part of the public sentry.LogEntry API.
type uint64LogEntry interface {
	Uint64(key string, value uint64) sentry.LogEntry
}

func logrusFieldToLogEntry(logEntry sentry.LogEntry, key string, value interface{}) sentry.LogEntry {
	switch val := value.(type) {
	case int8:
		return logEntry.Int64(key, int64(val))
	case int16:
		return logEntry.Int64(key, int64(val))
	case int32:
		return logEntry.Int64(key, int64(val))
	case int64:
		return logEntry.Int64(key, val)
	case int:
		return logEntry.Int64(key, int64(val))
	case uint, uint8, uint16, uint32, uint64:
		uval := reflect.ValueOf(val).Convert(reflect.TypeOf(uint64(0))).Uint()
		if e, ok := logEntry.(uint64LogEntry); ok {
			return e.Uint64(key, uval)
		}
		debuglog.Println("Internal error: log entry does not implement unsigned int conversion")
		return logEntry
	case string:
		return logEntry.String(key, val)
	case float32:
		return logEntry.Float64(key, float64(val))
	case float64:
		return logEntry.Float64(key, val)
	case bool:
		return logEntry.Bool(key, val)
	case time.Time:
		return logEntry.String(key, val.Format(time.RFC3339))
	case time.Duration:
		return logEntry.String(key, val.String())
	default:
		// Fallback to string conversion for unknown types
		return logEntry.String(key, fmt.Sprint(value))
	}
}

func (h *logHook) Fire(entry *logrus.Entry) error {
	ctx := context.Background()
	if entry.Context != nil {
		ctx = entry.Context
	}

	hub := sentry.GetHubFromContext(ctx)
	if h.useCustomProvider {
		if customHub := h.hubProvider(); customHub != nil && customHub.Client() != nil {
			hub = customHub
		}
	}
	if hub == nil {
		hub = h.defaultHub
	}
	ctx = sentry.SetHubOnContext(ctx, hub)

	logger := h.logger
	if hub.Client() != h.defaultHub.Client() {
		logger = sentry.NewLogger(ctx)
		logger.SetAttributes(h.attributes...)
	}

	// Create the base log entry for the appropriate level
	var logEntry sentry.LogEntry
	switch entry.Level {
	case logrus.TraceLevel:
		logEntry = logger.Trace().WithCtx(ctx)
	case logrus.DebugLevel:
		logEntry = logger.Debug().WithCtx(ctx)
	case logrus.InfoLevel:
		logEntry = logger.Info().WithCtx(ctx)
	case logrus.WarnLevel:
		logEntry = logger.Warn().WithCtx(ctx)
	case logrus.ErrorLevel:
		logEntry = logger.Error().WithCtx(ctx)
	case logrus.FatalLevel:
		logEntry = logger.LFatal().WithCtx(ctx)
	case logrus.PanicLevel:
		logEntry = logger.LFatal().WithCtx(ctx)
	default:
		debuglog.Printf("Invalid logrus logging level: %v. Dropping log.", entry.Level)
		if h.fallback != nil {
			return h.fallback(entry)
		}
		return errors.New("invalid log level")
	}

	// Add all the fields as attributes to this specific log entry
	for k, v := range entry.Data {
		// Skip specific fields that might be handled separately
		if k == FieldGoVersion || k == FieldMaxProcs {
			continue
		}

		logEntry = logrusFieldToLogEntry(logEntry, h.key(k), v)
	}

	// Emit the log entry with the message
	logEntry.Emit(entry.Message)
	return nil
}

func (h *logHook) Levels() []logrus.Level {
	return h.levels
}

func (h *logHook) Flush(timeout time.Duration) bool {
	return h.hubProvider().Client().Flush(timeout)
}

func (h *logHook) FlushWithContext(ctx context.Context) bool {
	return h.hubProvider().Client().FlushWithContext(ctx)
}

// NewLogHook initializes a new Logrus hook which sends logs to a new Sentry client
// configured according to opts.
func NewLogHook(levels []logrus.Level, opts sentry.ClientOptions) (Hook, error) {
	if opts.DisableLogs {
		return nil, errors.New("cannot create log hook, DisableLogs is set to true")
	}
	client, err := sentry.NewClient(opts)
	if err != nil {
		return nil, err
	}

	client.SetSDKIdentifier(sdkIdentifier)
	return NewLogHookFromClient(levels, client), nil
}

// NewLogHookFromClient initializes a new Logrus hook which sends logs to the provided
// sentry client.
func NewLogHookFromClient(levels []logrus.Level, client *sentry.Client) Hook {
	defaultHub := sentry.NewHub(client, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), defaultHub)
	logger := sentry.NewLogger(ctx)
	origin := attribute.String("sentry.origin", LogrusOrigin)
	logger.SetAttributes(origin)

	return &logHook{
		defaultHub: defaultHub,
		levels:     levels,
		hubProvider: func() *sentry.Hub {
			// Default to using the same hub if no specific provider is set
			return defaultHub
		},
		keys:       make(map[string]string),
		attributes: []attribute.Builder{origin},
		logger:     logger,
	}
}
