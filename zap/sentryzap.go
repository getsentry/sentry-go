// Package sentryzap provides a zap Core implementation for sending logs to Sentry.
package sentryzap

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"go.uber.org/zap/zapcore"
)

const (
	// ZapOrigin is the Sentry origin attribute value for zap logs.
	ZapOrigin = "auto.log.zap"
)

// Ensure SentryCore implements zapcore.Core.
var _ zapcore.Core = (*SentryCore)(nil)

// Option configures the Sentry zap Core.
type Option struct {
	// Level specifies the zap levels to capture and send to Sentry as log entries.
	// Only logs at these specific levels will be processed.
	// Defaults to all levels: Debug, Info, Warn, Error, DPanic, Panic, Fatal.
	Level []zapcore.Level

	// AddCaller includes caller information (file, line, function) in logs.
	// Defaults to false.
	AddCaller bool

	// FlushTimeout specifies how long to wait when syncing/flushing logs.
	// Defaults to 5 seconds.
	FlushTimeout time.Duration
}

// SentryCore is a zapcore.Core implementation that sends logs to Sentry.
type SentryCore struct {
	option Option
	logger sentry.Logger
	fields []zapcore.Field
	ctx    context.Context
}

// NewSentryCore creates a new zapcore.Core that sends logs to Sentry.
func NewSentryCore(ctx context.Context, opts Option) *SentryCore {
	if opts.Level == nil {
		opts.Level = []zapcore.Level{
			zapcore.DebugLevel,
			zapcore.InfoLevel,
			zapcore.WarnLevel,
			zapcore.ErrorLevel,
			zapcore.DPanicLevel,
			zapcore.PanicLevel,
			zapcore.FatalLevel,
		}
	}
	if opts.FlushTimeout == 0 {
		opts.FlushTimeout = 5 * time.Second
	}

	logger := sentry.NewLogger(ctx)
	logger.SetAttributes(attribute.String("sentry.origin", ZapOrigin))

	return &SentryCore{
		option: opts,
		logger: logger,
		fields: []zapcore.Field{},
		ctx:    ctx,
	}
}

// Context returns a zapcore.Field that can be used with logger.With() to link
// traces with the provided context. This allows propagating Sentry trace information
// from the context to logs without needing to pass a Hub.
//
// Example:
//
//	logger := zap.New(sentryzap.NewSentryCore(ctx, sentryzap.Option{}))
//	logger = logger.With(sentryzap.Context(requestCtx))
//	logger.Info("handling request") // This log will be linked to the trace in requestCtx
func Context(ctx context.Context) zapcore.Field {
	return zapcore.Field{
		Key:       "_sentry_context",
		Type:      zapcore.SkipType,
		Interface: ctx,
	}
}

// Enabled returns true if the given level is in the configured Level list.
func (c *SentryCore) Enabled(level zapcore.Level) bool {
	for _, l := range c.option.Level {
		if l == level {
			return true
		}
	}
	return false
}

// With returns a new Core with the given fields added to the context.
func (c *SentryCore) With(fields []zapcore.Field) zapcore.Core {
	newCtx := c.ctx
	var filteredFields []zapcore.Field

	for _, field := range fields {
		if field.Key == "_sentry_context" && field.Type == zapcore.SkipType {
			if ctx, ok := field.Interface.(context.Context); ok {
				newCtx = ctx
				continue
			}
		}
		filteredFields = append(filteredFields, field)
	}

	newFields := make([]zapcore.Field, len(c.fields), len(c.fields)+len(filteredFields))
	copy(newFields, c.fields)
	newFields = append(newFields, filteredFields...)

	return &SentryCore{
		option: c.option,
		logger: c.logger,
		fields: newFields,
		ctx:    newCtx,
	}
}

// Check determines whether the supplied Entry should be logged.
func (c *SentryCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

// Write serializes the Entry and any Fields and sends them to Sentry.
func (c *SentryCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	logEntry := c.getLogEntry(entry.Level, c.ctx)
	if logEntry == nil {
		return nil
	}

	if c.option.AddCaller && entry.Caller.Defined {
		logEntry = logEntry.String("code.filepath", entry.Caller.File)
		logEntry = logEntry.Int("code.lineno", entry.Caller.Line)
		if entry.Caller.Function != "" {
			logEntry = logEntry.String("code.function", entry.Caller.Function)
		}
	}
	if entry.LoggerName != "" {
		logEntry = logEntry.String("logger.name", entry.LoggerName)
	}
	if entry.Stack != "" {
		logEntry = logEntry.String("exception.stacktrace", entry.Stack)
	}

	// Convert and add accumulated fields from With()
	for _, field := range c.fields {
		logEntry = zapFieldToLogEntry(logEntry, field)
	}
	// Convert and add fields from this specific log call
	for _, field := range fields {
		logEntry = zapFieldToLogEntry(logEntry, field)
	}
	logEntry.Emit(entry.Message)
	return nil
}

// Sync flushes any buffered log entries to Sentry.
func (c *SentryCore) Sync() error {
	hub := sentry.GetHubFromContext(c.ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	if ok := hub.Flush(c.option.FlushTimeout); !ok {
		return fmt.Errorf("failed to flush client: %v", hub.Client())
	}
	return nil
}

// getLogEntry returns the appropriate sentry.LogEntry for the given zap level.
func (c *SentryCore) getLogEntry(level zapcore.Level, ctx context.Context) sentry.LogEntry {
	var logEntry sentry.LogEntry

	switch level {
	case zapcore.DebugLevel:
		logEntry = c.logger.Debug()
	case zapcore.InfoLevel:
		logEntry = c.logger.Info()
	case zapcore.WarnLevel:
		logEntry = c.logger.Warn()
	case zapcore.ErrorLevel:
		logEntry = c.logger.Error()
	case zapcore.DPanicLevel:
		// DPanic is treated as Error in production
		logEntry = c.logger.Error()
	case zapcore.PanicLevel:
		logEntry = c.logger.LFatal()
	case zapcore.FatalLevel:
		logEntry = c.logger.LFatal()
	default:
		// For any other level, use Info
		logEntry = c.logger.Info()
	}

	return logEntry.WithCtx(ctx)
}
