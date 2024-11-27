package sentryslog

import (
	"context"
	"log/slog"

	"github.com/getsentry/sentry-go"
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
	// Level sets the minimum log level to capture and send to Sentry.
	// Logs at this level and above will be processed. The default level is debug.
	Level slog.Leveler

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

func (o Option) NewSentryHandler() slog.Handler {
	if o.Level == nil {
		o.Level = slog.LevelDebug
	}

	if o.Converter == nil {
		o.Converter = DefaultConverter
	}

	if o.AttrFromContext == nil {
		o.AttrFromContext = []func(ctx context.Context) []slog.Attr{}
	}

	return &SentryHandler{
		option: o,
		attrs:  []slog.Attr{},
		groups: []string{},
	}
}

type SentryHandler struct {
	option Option
	attrs  []slog.Attr
	groups []string
}

func (h *SentryHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.option.Level.Level()
}

func (h *SentryHandler) Handle(ctx context.Context, record slog.Record) error {
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

func (h *SentryHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SentryHandler{
		option: h.option,
		attrs:  appendAttrsToGroup(h.groups, h.attrs, attrs...),
		groups: h.groups,
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
	}
}
