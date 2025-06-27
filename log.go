package sentry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/attribute"
)

type LogLevel string

const (
	LogLevelTrace LogLevel = "trace"
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
	LogLevelFatal LogLevel = "fatal"
)

const (
	LogSeverityTrace   int = 1
	LogSeverityDebug   int = 5
	LogSeverityInfo    int = 9
	LogSeverityWarning int = 13
	LogSeverityError   int = 17
	LogSeverityFatal   int = 21
)

var mapTypesToStr = map[attribute.Type]string{
	attribute.INVALID: "",
	attribute.BOOL:    "boolean",
	attribute.INT64:   "integer",
	attribute.FLOAT64: "double",
	attribute.STRING:  "string",
}

type sentryLogger struct {
	client     *Client
	attributes map[string]Attribute
}

type logEntry struct {
	logger      *sentryLogger
	ctx         context.Context
	level       LogLevel
	severity    int
	attributes  map[string]Attribute
	shouldPanic bool
}

// NewLogger returns a Logger that emits logs to Sentry. If logging is turned off, all logs get discarded.
func NewLogger(ctx context.Context) Logger {
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.batchLogger != nil {
		return &sentryLogger{client, make(map[string]Attribute)}
	}

	DebugLogger.Println("fallback to noopLogger: enableLogs disabled")
	return &noopLogger{} // fallback: does nothing
}

func (l *sentryLogger) Write(p []byte) (int, error) {
	// Avoid sending double newlines to Sentry
	msg := strings.TrimRight(string(p), "\n")
	l.Info().WithCtx(context.Background()).Emit(msg)
	return len(p), nil
}

func (l *sentryLogger) log(ctx context.Context, level LogLevel, severity int, message string, entryAttrs map[string]Attribute, args ...interface{}) {
	if message == "" {
		return
	}
	hub := GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	var traceID TraceID
	var spanID SpanID

	span := hub.Scope().span
	if span != nil {
		traceID = span.TraceID
		spanID = span.SpanID
	} else {
		traceID = hub.Scope().propagationContext.TraceID
	}

	attrs := map[string]Attribute{}
	if len(args) > 0 {
		attrs["sentry.message.template"] = Attribute{
			Value: message, Type: "string",
		}
		for i, p := range args {
			attrs[fmt.Sprintf("sentry.message.parameters.%d", i)] = Attribute{
				Value: fmt.Sprint(p), Type: "string",
			}
		}
	}

	for k, v := range l.attributes {
		attrs[k] = v
	}
	for k, v := range entryAttrs {
		attrs[k] = v
	}

	// Set default attributes
	if release := l.client.options.Release; release != "" {
		attrs["sentry.release"] = Attribute{Value: release, Type: "string"}
	}
	if environment := l.client.options.Environment; environment != "" {
		attrs["sentry.environment"] = Attribute{Value: environment, Type: "string"}
	}
	if serverName := l.client.options.ServerName; serverName != "" {
		attrs["sentry.server.address"] = Attribute{Value: serverName, Type: "string"}
	} else if serverAddr, err := os.Hostname(); err == nil {
		attrs["sentry.server.address"] = Attribute{Value: serverAddr, Type: "string"}
	}
	scope := hub.Scope()
	if scope != nil {
		user := scope.user
		if !user.IsEmpty() {
			if user.ID != "" {
				attrs["user.id"] = Attribute{Value: user.ID, Type: "string"}
			}
			if user.Name != "" {
				attrs["user.name"] = Attribute{Value: user.Name, Type: "string"}
			}
			if user.Email != "" {
				attrs["user.email"] = Attribute{Value: user.Email, Type: "string"}
			}
		}
	}
	if spanID.String() != "0000000000000000" {
		attrs["sentry.trace.parent_span_id"] = Attribute{Value: spanID.String(), Type: "string"}
	}
	if sdkIdentifier := l.client.sdkIdentifier; sdkIdentifier != "" {
		attrs["sentry.sdk.name"] = Attribute{Value: sdkIdentifier, Type: "string"}
	}
	if sdkVersion := l.client.sdkVersion; sdkVersion != "" {
		attrs["sentry.sdk.version"] = Attribute{Value: sdkVersion, Type: "string"}
	}

	log := &Log{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		Level:      level,
		Severity:   severity,
		Body:       fmt.Sprintf(message, args...),
		Attributes: attrs,
	}

	if l.client.options.BeforeSendLog != nil {
		log = l.client.options.BeforeSendLog(log)
	}

	if log != nil {
		l.client.batchLogger.logCh <- *log
	}

	if l.client.options.Debug {
		DebugLogger.Printf(message, args...)
	}
}

// SetAttributes permanently attaches attributes to the logger instance
func (l *sentryLogger) SetAttributes(attrs ...attribute.Builder) {
	for _, v := range attrs {
		t, ok := mapTypesToStr[v.Value.Type()]
		if !ok || t == "" {
			DebugLogger.Printf("invalid attribute type set: %v", t)
			continue
		}

		l.attributes[v.Key] = Attribute{
			Value: v.Value.AsInterface(),
			Type:  t,
		}
	}
}

func (l *sentryLogger) Trace() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelTrace,
		severity:   LogSeverityTrace,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Debug() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelDebug,
		severity:   LogSeverityDebug,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Info() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelInfo,
		severity:   LogSeverityInfo,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Warn() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelWarn,
		severity:   LogSeverityWarning,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Error() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelError,
		severity:   LogSeverityError,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Fatal() LogEntry {
	return &logEntry{
		logger:     l,
		ctx:        context.Background(),
		level:      LogLevelFatal,
		severity:   LogSeverityFatal,
		attributes: make(map[string]Attribute),
	}
}

func (l *sentryLogger) Panic() LogEntry {
	return &logEntry{
		logger:      l,
		ctx:         context.Background(),
		level:       LogLevelFatal,
		severity:    LogSeverityFatal,
		attributes:  make(map[string]Attribute),
		shouldPanic: true, // this should panic instead of exit
	}
}

// WithCtx sets the context for this log entry
func (e *logEntry) WithCtx(ctx context.Context) LogEntry {
	e.ctx = ctx
	return e
}

func (e *logEntry) String(key, value string) LogEntry {
	e.attributes[key] = Attribute{Value: value, Type: "string"}
	return e
}

func (e *logEntry) Int(key string, value int) LogEntry {
	e.attributes[key] = Attribute{Value: int64(value), Type: "integer"}
	return e
}

func (e *logEntry) Int64(key string, value int64) LogEntry {
	e.attributes[key] = Attribute{Value: value, Type: "integer"}
	return e
}

func (e *logEntry) Float64(key string, value float64) LogEntry {
	e.attributes[key] = Attribute{Value: value, Type: "double"}
	return e
}

func (e *logEntry) Bool(key string, value bool) LogEntry {
	e.attributes[key] = Attribute{Value: value, Type: "boolean"}
	return e
}

// Attributes method for adding multiple attributes at once
func (e *logEntry) Attributes(attrs ...attribute.Builder) LogEntry {
	for _, v := range attrs {
		t, ok := mapTypesToStr[v.Value.Type()]
		if !ok || t == "" {
			DebugLogger.Printf("invalid attribute type set: %v", t)
			continue
		}
		e.attributes[v.Key] = Attribute{
			Value: v.Value.AsInterface(),
			Type:  t,
		}
	}
	return e
}

// Emit sends the log entry with the specified message
func (e *logEntry) Emit(args ...interface{}) {
	e.logger.log(e.ctx, e.level, e.severity, fmt.Sprint(args...), e.attributes)

	if e.level == LogLevelFatal {
		if e.shouldPanic {
			panic(fmt.Sprint(args...))
		} else {
			os.Exit(1)
		}
	}
}

// Emitf sends the log entry with a formatted message
func (e *logEntry) Emitf(format string, args ...interface{}) {
	e.logger.log(e.ctx, e.level, e.severity, format, e.attributes, args...)

	if e.level == LogLevelFatal {
		if e.shouldPanic {
			formattedMessage := fmt.Sprintf(format, args...)
			panic(formattedMessage)
		} else {
			os.Exit(1)
		}
	}
}
