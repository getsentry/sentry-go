package sentry

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/getsentry/sentry-go/attribute"
)

type SlogHandler struct {
	logger     Logger
	attributes []slog.Attr
	groups     []string
	opts       slog.HandlerOptions
}

func slogAttrToSentryAttr(group string, a slog.Attr) []attribute.Builder {
	switch a.Value.Kind() {
	case slog.KindAny:
		return []attribute.Builder{attribute.String(group+a.Key, fmt.Sprintf("%+v", a.Value.Any()))}
	case slog.KindBool:
		return []attribute.Builder{attribute.Bool(group+a.Key, a.Value.Bool())}
	case slog.KindDuration:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.Duration().String())}
	case slog.KindFloat64:
		return []attribute.Builder{attribute.Float64(group+a.Key, a.Value.Float64())}
	case slog.KindInt64:
		return []attribute.Builder{attribute.Int64(group+a.Key, a.Value.Int64())}
	case slog.KindString:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.String())}
	case slog.KindTime:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.Time().Format(time.RFC3339))}
	case slog.KindUint64:
		// TODO: currently Relay cannot process uint64, try to convert to a supported format.
		val := a.Value.Uint64()
		if val <= math.MaxInt64 {
			return []attribute.Builder{attribute.Int64(a.Key, int64(val))}
		} else {
			// For values larger than int64 can handle, we are using float. Potential precision loss
			return []attribute.Builder{attribute.Float64(a.Key, float64(val))}
		}
	case slog.KindLogValuer:
		return []attribute.Builder{attribute.String(group+a.Key, a.Value.LogValuer().LogValue().String())}
	case slog.KindGroup:
		// Handle nested group attributes
		var attrs []attribute.Builder
		groupPrefix := group + a.Key
		if groupPrefix != "" {
			groupPrefix += "."
		}
		for _, subAttr := range a.Value.Group() {
			attrs = append(attrs, slogAttrToSentryAttr(groupPrefix, subAttr)...)
		}
		return attrs
	}

	DebugLogger.Printf("Invalid type: dropping attribute with key: %v and value: %v", a.Key, a.Value)
	return []attribute.Builder{}
}

// Enabled implements slog.Handler.
func (s *SlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if s.opts.Level != nil {
		minLevel = s.opts.Level.Level()
	}

	return level >= minLevel
}

// Handle implements slog.Handler.
func (s *SlogHandler) Handle(ctx context.Context, record slog.Record) error {
	sentryAttributes := make([]attribute.Builder, 0)
	replaceAttr := s.opts.ReplaceAttr

	// Process handler attributes first, should not apply groups here.
	for _, attr := range s.attributes {
		a := attr
		if replaceAttr != nil {
			a = replaceAttr([]string{}, a)
		}
		sentryAttributes = append(sentryAttributes, slogAttrToSentryAttr("", a)...)
	}

	groupPrepend := s.createGroupKey()
	record.Attrs(func(a slog.Attr) bool {
		if replaceAttr != nil {
			a = replaceAttr([]string{}, a)
		}
		sentryAttributes = append(sentryAttributes, slogAttrToSentryAttr(groupPrepend, a)...)
		return true
	})

	s.logger.SetAttributes(sentryAttributes...)
	switch record.Level {
	case slog.LevelDebug:
		s.logger.Debug(ctx, record.Message)
	case slog.LevelInfo:
		s.logger.Info(ctx, record.Message)
	case slog.LevelWarn:
		s.logger.Warn(ctx, record.Message)
	case slog.LevelError:
		s.logger.Error(ctx, record.Message)
	}

	return nil
}

// WithAttrs implements slog.Handler.
func (s *SlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandler := &SlogHandler{
		logger:     s.logger,
		attributes: append([]slog.Attr{}, s.attributes...),
		groups:     append([]string{}, s.groups...),
		opts:       s.opts,
	}

	groupPrepend := s.createGroupKey()
	for _, attr := range attrs {
		// apply group key
		attr.Key = groupPrepend + attr.Key
		newHandler.attributes = append(newHandler.attributes, attr)
	}

	return newHandler
}

// WithGroup implements slog.Handler.
func (s *SlogHandler) WithGroup(name string) slog.Handler {
	if len(name) == 0 {
		return s
	}
	newHandler := &SlogHandler{
		logger:     s.logger,
		attributes: append([]slog.Attr{}, s.attributes...),
		groups:     append([]string{}, s.groups...),
		opts:       s.opts,
	}

	newHandler.groups = append(newHandler.groups, name)
	return newHandler
}

func (s *SlogHandler) createGroupKey() string {
	var groupPrepend string
	if len(s.groups) > 0 {
		groupPrepend = ""
		for _, group := range s.groups {
			if groupPrepend != "" {
				groupPrepend += "."
			}
			groupPrepend += group
		}
		groupPrepend += "."
	}
	return groupPrepend
}

var _ slog.Handler = (*SlogHandler)(nil)

func NewSentrySlogHandler(ctx context.Context, opts *slog.HandlerOptions) slog.Handler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}

	return &SlogHandler{
		logger:     NewLogger(ctx),
		attributes: make([]slog.Attr, 0),
		opts:       *opts,
	}
}
