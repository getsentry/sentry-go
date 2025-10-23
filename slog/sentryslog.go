package sentryslog

import (
	"context"
	"errors"
	"log/slog"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

// Majority of the code in this package is derived from https://github.com/samber/slog-sentry AND https://github.com/samber/slog-common
// MIT License

// Copyright (c) 2023 Samuel Berthe

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

var (
	_ slog.Handler = (*SentryHandler)(nil)

	LogLevels = map[slog.Level]sentry.Level{
		slog.LevelDebug: sentry.LevelDebug,
		slog.LevelInfo:  sentry.LevelInfo,
		slog.LevelWarn:  sentry.LevelWarning,
		slog.LevelError: sentry.LevelError,
		LevelFatal:      sentry.LevelFatal,
	}
)

// LevelFatal is a custom [slog.Level] that maps to [sentry.LevelFatal]
const LevelFatal = slog.Level(12)
const SlogOrigin = "auto.logger.slog"

type Option struct {
	// Deprecated: Use EventLevel instead. Level is kept for backwards compatibility and defaults to EventLevel.
	Level slog.Leveler
	// EventLevel specifies the exact log levels to capture and send to Sentry as Events.
	// Only logs at these specific levels will be processed as events.
	// Defaults to []slog.Level{slog.LevelError, LevelFatal}.
	EventLevel []slog.Level

	// LogLevel specifies the exact log levels to capture and send to Sentry as Log entries.
	// Only logs at these specific levels will be processed as log entries.
	// Defaults to []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}.
	LogLevel []slog.Level

	// Hub specifies the Sentry Hub to use for capturing events.
	// If not provided, the current Hub is used by default.
	Hub *sentry.Hub

	// Converter is an optional function that customizes how log records
	// are converted into Sentry events. By default, the DefaultConverter is used.
	Converter Converter

	// AttrFromContext is an optional slice of functions that extract attributes
	// from the context. These functions can add additional metadata to the log entry.
	AttrFromContext []func(ctx context.Context) []slog.Attr

	// AddSource is an optional flag that, when set to true, includes the source
	// information (such as file and line number) in the Sentry event.
	// This can be useful for debugging purposes.
	AddSource bool

	// ReplaceAttr is an optional function that allows for the modification or
	// replacement of attributes in the log record. This can be used to filter
	// or transform attributes before they are sent to Sentry.
	ReplaceAttr func(groups []string, a slog.Attr) slog.Attr
}

func (o Option) NewSentryHandler(ctx context.Context) slog.Handler {
	if o.EventLevel == nil {
		// backwards compatibility
		if o.Level != nil {
			o.EventLevel = levelsFromMinimum(o.Level.Level())
		} else {
			o.EventLevel = []slog.Level{slog.LevelError, LevelFatal}
		}
	}
	if o.LogLevel == nil {
		o.LogLevel = []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}
	}

	if o.Converter == nil {
		o.Converter = DefaultConverter
	}

	if o.AttrFromContext == nil {
		o.AttrFromContext = []func(ctx context.Context) []slog.Attr{}
	}

	logger := sentry.NewLogger(ctx)
	logger.SetAttributes(attribute.String("sentry.origin", SlogOrigin))

	eventHandler := &eventHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}
	logHandler := &logHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
		logger: logger,
	}

	return &SentryHandler{
		eventHandler: eventHandler,
		logHandler:   logHandler,
	}
}

type SentryHandler struct {
	eventHandler *eventHandler
	logHandler   *logHandler
}

func (h *SentryHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.eventHandler.Enabled(ctx, level) || h.logHandler.Enabled(ctx, level)
}

func (h *SentryHandler) Handle(ctx context.Context, record slog.Record) error {
	var err error

	if h.eventHandler.Enabled(ctx, record.Level) {
		err = errors.Join(err, h.eventHandler.Handle(ctx, record))
	}

	if h.logHandler.Enabled(ctx, record.Level) {
		err = errors.Join(err, h.logHandler.Handle(ctx, record))
	}

	return err
}

func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SentryHandler{
		eventHandler: h.eventHandler.WithAttrs(attrs),
		logHandler:   h.logHandler.WithAttrs(attrs),
	}
}

func (h *SentryHandler) WithGroup(name string) slog.Handler {
	return &SentryHandler{
		eventHandler: h.eventHandler.WithGroup(name),
		logHandler:   h.logHandler.WithGroup(name),
	}
}

type eventHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
}

func (h *eventHandler) Enabled(_ context.Context, level slog.Level) bool {
	for _, eventLevel := range h.option.EventLevel {
		if level == eventLevel {
			return true
		}
	}
	return false
}

func (h *eventHandler) Handle(ctx context.Context, record slog.Record) error {
	hub := sentry.CurrentHub()
	if hubFromContext := sentry.GetHubFromContext(ctx); hubFromContext != nil {
		hub = hubFromContext
	} else if h.option.Hub != nil {
		hub = h.option.Hub
	}

	fromContext := contextExtractor(ctx, h.option.AttrFromContext)
	event := h.option.Converter(h.option.AddSource, h.option.ReplaceAttr, append(h.attrs, fromContext...), h.groups, &record, hub)
	hub.CaptureEvent(event)
	return nil
}

func (h *eventHandler) WithAttrs(attrs []slog.Attr) *eventHandler {
	// Create a copy of the groups slice to avoid sharing state
	groupsCopy := make([]string, len(h.groups))
	copy(groupsCopy, h.groups)

	return &eventHandler{
		option: h.option,
		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: groupsCopy,
	}
}

func (h *eventHandler) WithGroup(name string) *eventHandler {
	if name == "" {
		return h
	}

	// Create a copy of the groups slice to avoid modifying the original
	newGroups := make([]string, len(h.groups), len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups = append(newGroups, name)

	return &eventHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: newGroups,
	}
}

type logHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
	logger sentry.Logger
}

func (h *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	for _, logLevel := range h.option.LogLevel {
		if level == logLevel {
			return true
		}
	}
	return false
}

func (h *logHandler) Handle(ctx context.Context, record slog.Record) error {
	// aggregate all attributes
	attrs := appendRecordAttrsToAttrs(h.attrs, h.groups, &record)
	if h.option.AddSource {
		attrs = append(attrs, source(sourceKey, &record))
	}
	attrs = replaceAttrs(h.option.ReplaceAttr, []string{}, attrs...)
	attrs = removeEmptyAttrs(attrs)

	// Use level ranges instead of exact matches to support custom levels
	switch {
	case record.Level < slog.LevelDebug:
		// Levels below Debug (e.g., Trace)
		logEntry := h.logger.Trace().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	case record.Level < slog.LevelInfo:
		// Debug level range: -4 to -1
		logEntry := h.logger.Debug().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	case record.Level < slog.LevelWarn:
		// Info level range: 0 to 3
		logEntry := h.logger.Info().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	case record.Level < slog.LevelError:
		// Warn level range: 4 to 7
		logEntry := h.logger.Warn().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	case record.Level < LevelFatal: // custom Fatal level, keep +4 increments
		logEntry := h.logger.Error().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	default:
		// Fatal level range: 12 and above
		logEntry := h.logger.Fatal().WithCtx(ctx)
		for _, attr := range attrs {
			logEntry = slogAttrToLogEntry(logEntry, "", attr)
		}
		logEntry.Emit(record.Message)
	}

	return nil
}

func (h *logHandler) WithAttrs(attrs []slog.Attr) *logHandler {
	// Create a copy of the groups slice to avoid sharing state
	groupsCopy := make([]string, len(h.groups))
	copy(groupsCopy, h.groups)

	return &logHandler{
		option: h.option,
		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: groupsCopy,
		logger: h.logger,
	}
}

func (h *logHandler) WithGroup(name string) *logHandler {
	if name == "" {
		return h
	}

	// Create a copy of the groups slice to avoid modifying the original
	newGroups := make([]string, len(h.groups), len(h.groups)+1)
	copy(newGroups, h.groups)
	newGroups = append(newGroups, name)

	return &logHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: newGroups,
		logger: h.logger,
	}
}

func levelsFromMinimum(minLevel slog.Level) []slog.Level {
	allLevels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}
	var result []slog.Level
	for _, level := range allLevels {
		if level >= minLevel {
			result = append(result, level)
		}
	}
	return result
}
