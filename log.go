package sentry

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/attribute"
)

// LogLevel marks the severity of the log event.
type LogLevel string

const (
	LogLevelTrace   Level = "trace"
	LogLevelDebug   Level = "debug"
	LogLevelInfo    Level = "info"
	LogLevelWarning Level = "warning"
	LogLevelError   Level = "error"
	LogLevelFatal   Level = "fatal"
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

// sentryLogger implements a custom logger that writes to Sentry.
type sentryLogger struct {
	hub     *Hub
	client  *Client
	options LoggerOptions
}

type LoggerOptions struct {
	// BeforeSendLog is called before a log event is sent to Sentry.
	// Use it to mutate the log event or return nil to discard.
	BeforeSendLog func(log *Log) *Log
}

// NewLogger returns a Logger that writes to Sentry if enabled, or discards otherwise.
func NewLogger(ctx context.Context, opts LoggerOptions) Logger {
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.batchLogger != nil {
		return &sentryLogger{hub, client, opts}
	}
	return &noopLogger{} // fallback: does nothing
}

func (l *sentryLogger) Write(p []byte) (n int, err error) {
	// Avoid sending double newlines to Sentry
	msg := strings.TrimRight(string(p), "\n")
	err = l.log(LevelInfo, LogSeverityInfo, msg)
	return len(p), err
}

func (l *sentryLogger) log(level Level, severity int, args ...interface{}) error {
	if len(args) == 0 {
		return nil
	}
	traceParent := l.hub.GetTraceparent()
	var traceID TraceID
	_, err := hex.Decode(traceID[:], []byte(traceParent[:32]))
	if err != nil {
		return err
	}

	var template string
	var message string
	var parameters []interface{}
	attrs := map[string]Attribute{}

	for _, arg := range args {
		switch a := arg.(type) {
		case string:
			if template == "" {
				template = a
			} else {
				parameters = append(parameters, a)
			}
		case attribute.Builder:
			attrs[a.Key] = Attribute{
				Value: a.Value.AsInterface(),
				Type:  mapTypesToStr[a.Value.Type()],
			}
		default:
			parameters = append(parameters, a)
		}
	}

	if template != "" && len(parameters) > 0 {
		message = fmt.Sprintf(template, parameters...)
	} else {
		message = template
	}
	if template != "" && len(parameters) > 0 {
		attrs["sentry.message.template"] = Attribute{
			Value: template, Type: "string",
		}
		for i, p := range parameters {
			attrs[fmt.Sprintf("sentry.message.parameters.%d", i)] = Attribute{
				Value: fmt.Sprint(p), Type: "string",
			}
		}
	}

	// handle metadata
	if release := l.client.options.Release; release != "" {
		attrs["sentry.release"] = Attribute{Value: release, Type: "string"}
	}
	if environment := l.client.options.Environment; environment != "" {
		attrs["sentry.environment"] = Attribute{Value: environment, Type: "string"}
	}
	if serverAddr := l.client.options.ServerName; serverAddr != "" {
		attrs["sentry.server.address"] = Attribute{Value: serverAddr, Type: "string"}
	}
	if traceParent != "" {
		attrs["sentry.trace.parent_span_id"] = Attribute{Value: traceParent[33:], Type: "string"}
	}
	if sdkIdentifier := l.client.sdkIdentifier; sdkIdentifier != "" {
		attrs["sentry.sdk.name"] = Attribute{Value: sdkIdentifier, Type: "string"}
	}
	if sdkVersion := l.client.sdkVersion; sdkVersion != "" {
		attrs["sentry.sdk.version"] = Attribute{Value: sdkVersion, Type: "string"}
	}
	attrs["sentry.origin"] = Attribute{Value: "auto.logger.log", Type: "string"}

	log := &Log{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		Level:      level,
		Severity:   severity,
		Body:       message,
		Attributes: attrs,
	}
	if l.options.BeforeSendLog != nil {
		log = l.options.BeforeSendLog(log)
	}

	if log != nil {
		l.client.batchLogger.logCh <- *log
	}
	return nil
}

func (l *sentryLogger) Trace(v ...interface{}) { _ = l.log(LogLevelTrace, LogSeverityTrace, v...) }
func (l *sentryLogger) Debug(v ...interface{}) { _ = l.log(LogLevelDebug, LogSeverityDebug, v...) }
func (l *sentryLogger) Info(v ...interface{})  { _ = l.log(LogLevelInfo, LogSeverityInfo, v...) }
func (l *sentryLogger) Warn(v ...interface{})  { _ = l.log(LogLevelWarning, LogSeverityWarning, v...) }
func (l *sentryLogger) Error(v ...interface{}) { _ = l.log(LogLevelError, LogSeverityError, v...) }
func (l *sentryLogger) Fatal(v ...interface{}) {
	_ = l.log(LogLevelFatal, LogSeverityFatal, v...)
	os.Exit(1)
}
func (l *sentryLogger) Panic(v ...interface{}) {
	_ = l.log(LogLevelFatal, LogSeverityFatal, v...)
	panic(fmt.Sprint(v...))
}

// fallback no-op logger if Sentry is not enabled.
type noopLogger struct{}

func (*noopLogger) Trace(_ ...interface{}) {}
func (*noopLogger) Debug(_ ...interface{}) {}
func (*noopLogger) Info(_ ...interface{})  {}
func (*noopLogger) Warn(_ ...interface{})  {}
func (*noopLogger) Error(_ ...interface{}) {}
func (*noopLogger) Fatal(_ ...interface{}) { os.Exit(1) }
func (*noopLogger) Panic(_ ...interface{}) { panic("invalid setup: EnableLogs disabled") }
func (*noopLogger) Write(_ []byte) (n int, err error) {
	return 0, errors.New("invalid setup: EnableLogs disabled")
}
