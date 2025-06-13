package sentryslog

import (
	"context"
	"log/slog"
	"math"

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
	}
)

type Option struct {
	// Deprecated: Level is kept for backwards compatibility and defaults to EventLevel.
	Level slog.Leveler
	// EventLevel sets the minimum log level to capture and send to Sentry as an Event.
	// Logs at this level and above will be processed as events.
	// Defaults to slog.LevelError.
	EventLevel slog.Leveler

	// LogLevel sets the minimum log level to capture and send to Sentry as a Log entry.
	// Logs at this level and above will be processed as log entries.
	// Defaults to slog.LevelDebug.
	LogLevel slog.Leveler

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
			o.EventLevel = o.Level
		} else {
			o.EventLevel = slog.LevelError
		}
	}
	if o.LogLevel == nil {
		o.LogLevel = slog.LevelDebug
	}

	if o.Converter == nil {
		o.Converter = DefaultConverter
	}

	if o.AttrFromContext == nil {
		o.AttrFromContext = []func(ctx context.Context) []slog.Attr{}
	}

	logger := sentry.NewLogger(ctx)
	return &SentryHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
		logger: logger,
	}
}

type SentryHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
	logger sentry.Logger
}

func (h *SentryHandler) Enabled(_ context.Context, level slog.Level) bool {
	eventLevel := slog.Level(math.MaxInt)
	if h.option.EventLevel != nil {
		eventLevel = h.option.EventLevel.Level()
	}

	logLevel := slog.Level(math.MaxInt)
	if h.option.LogLevel != nil {
		logLevel = h.option.LogLevel.Level()
	}

	return level >= eventLevel || level >= logLevel
}

func (h *SentryHandler) Handle(ctx context.Context, record slog.Record) error {
	hub := sentry.CurrentHub()
	if hubFromContext := sentry.GetHubFromContext(ctx); hubFromContext != nil {
		hub = hubFromContext
	} else if h.option.Hub != nil {
		hub = h.option.Hub
	}

	if h.option.EventLevel != nil && record.Level >= h.option.EventLevel.Level() {
		if err := h.handleAsEvent(ctx, &record, hub); err != nil {
			return err
		}
	}

	if h.option.LogLevel != nil && record.Level >= h.option.LogLevel.Level() {
		if err := h.handleAsLog(ctx, &record, hub); err != nil {
			return err
		}
	}

	return nil
}

func (h *SentryHandler) handleAsEvent(ctx context.Context, record *slog.Record, hub *sentry.Hub) error {
	fromContext := contextExtractor(ctx, h.option.AttrFromContext)
	event := h.option.Converter(h.option.AddSource, h.option.ReplaceAttr, append(h.attrs, fromContext...), h.groups, record, hub)
	hub.CaptureEvent(event)
	return nil
}

func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SentryHandler{
		option: h.option,
		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
		logger: h.logger,
	}
}

func (h *SentryHandler) WithGroup(name string) slog.Handler {
	// https://cs.opensource.google/go/x/exp/+/46b07846:slog/handler.go;l=247
	if name == "" {
		return h
	}

	return &SentryHandler{
		option: h.option,
		attrs:  h.attrs,
		groups: append(h.groups, name),
		logger: h.logger,
	}
}

func (h *SentryHandler) handleAsLog(ctx context.Context, record *slog.Record, _ *sentry.Hub) error {
	// aggregate all attributes
	attrs := appendRecordAttrsToAttrs(h.attrs, h.groups, record)
	if h.option.AddSource {
		attrs = append(attrs, source(sourceKey, record))
	}
	attrs = replaceAttrs(h.option.ReplaceAttr, []string{}, attrs...)
	attrs = removeEmptyAttrs(attrs)

	var sentryAttributes []attribute.Builder
	for _, attr := range attrs {
		sentryAttributes = append(sentryAttributes, attrToSentryLog("", attr)...)
	}
	h.logger.SetAttributes(sentryAttributes...)
	h.logger.SetAttributes(attribute.String("sentry.origin", "auto.logger.slog"))
	switch record.Level {
	case slog.LevelDebug:
		h.logger.Debug(ctx, record.Message)
	case slog.LevelInfo:
		h.logger.Info(ctx, record.Message)
	case slog.LevelWarn:
		h.logger.Warn(ctx, record.Message)
	case slog.LevelError:
		h.logger.Error(ctx, record.Message)
	}

	return nil
}
