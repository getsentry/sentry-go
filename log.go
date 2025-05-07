package sentry

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// sentryLogger implements a custom logger that writes to Sentry.
type sentryLogger struct {
	hub    *Hub
	client *Client
}

// NewLogger returns a Logger that writes to Sentry if enabled, or discards otherwise.
func NewLogger(ctx context.Context) Logger {
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.batchLogger != nil {
		return &sentryLogger{hub, client}
	}
	return &noopLogger{} // fallback: does nothing
}

func (l *sentryLogger) Write(p []byte) (n int, err error) {
	// Avoid sending double newlines to Sentry
	msg := strings.TrimRight(string(p), "\n")
	err = l.log(LevelInfo, msg)
	return len(p), err
}

func (l *sentryLogger) log(level Level, args ...interface{}) error {
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
	attrs := map[string]any{}

	for _, arg := range args {
		switch a := arg.(type) {
		case string:
			if template == "" {
				template = a
			} else {
				parameters = append(parameters, a)
			}
		// case attribute.Builder:
		//	for k, v := range a {
		//		attrs[k] = v
		//	}
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
		attrs["sentry.release"] = release
	}
	if environment := l.client.options.Environment; environment != "" {
		attrs["sentry.environment"] = environment
	}
	if serverAddr := l.client.options.ServerName; serverAddr != "" {
		attrs["sentry.server.address"] = serverAddr
	}
	if traceParent != "" {
		attrs["sentry.trace.parent_span_id"] = traceParent[33:]
	}
	if sdkIdentifier := l.client.sdkIdentifier; sdkIdentifier != "" {
		attrs["sentry.sdk.name"] = sdkIdentifier
	}
	if sdkVersion := l.client.sdkVersion; sdkVersion != "" {
		attrs["sentry.sdk.version"] = sdkVersion
	}

	l.client.batchLogger.logCh <- Log{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		Level:      level,
		Body:       message,
		Attributes: attrs,
	}

	return nil
}

func (l *sentryLogger) Trace(v ...interface{}) { _ = l.log(LevelTrace, v...) }
func (l *sentryLogger) Debug(v ...interface{}) { _ = l.log(LevelDebug, v...) }
func (l *sentryLogger) Info(v ...interface{})  { _ = l.log(LevelInfo, v...) }
func (l *sentryLogger) Warn(v ...interface{})  { _ = l.log(LevelWarning, v...) }
func (l *sentryLogger) Error(v ...interface{}) { _ = l.log(LevelError, v...) }
func (l *sentryLogger) Fatal(v ...interface{}) { _ = l.log(LevelFatal, v...); os.Exit(1) }
func (l *sentryLogger) Panic(v ...interface{}) { _ = l.log(LevelFatal, v...); panic(fmt.Sprint(v...)) }

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
