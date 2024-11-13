package sentryslog

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestSentryHandler_Enabled(t *testing.T) {
	tests := map[string]struct {
		handlerLevel slog.Level
		checkLevel   slog.Level
		expected     bool
	}{
		"LevelDebug, CheckDebug": {
			handlerLevel: slog.LevelDebug,
			checkLevel:   slog.LevelDebug,
			expected:     true,
		},
		"LevelInfo, CheckDebug": {
			handlerLevel: slog.LevelInfo,
			checkLevel:   slog.LevelDebug,
			expected:     false,
		},
		"LevelError, CheckWarn": {
			handlerLevel: slog.LevelError,
			checkLevel:   slog.LevelWarn,
			expected:     false,
		},
		"LevelWarn, CheckError": {
			handlerLevel: slog.LevelWarn,
			checkLevel:   slog.LevelError,
			expected:     true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := SentryHandler{option: Option{Level: tt.handlerLevel}}
			if got := h.Enabled(context.Background(), tt.checkLevel); got != tt.expected {
				t.Errorf("Enabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSentryHandler_WithAttrs(t *testing.T) {
	tests := map[string]struct {
		initialAttrs []slog.Attr
		newAttrs     []slog.Attr
		expected     []slog.Attr
	}{
		"Empty initial attrs": {
			initialAttrs: []slog.Attr{},
			newAttrs:     []slog.Attr{slog.String("key", "value")},
			expected:     []slog.Attr{slog.String("key", "value")},
		},
		"Non-empty initial attrs": {
			initialAttrs: []slog.Attr{slog.String("existing", "attr")},
			newAttrs:     []slog.Attr{slog.String("key", "value")},
			expected:     []slog.Attr{slog.String("existing", "attr"), slog.String("key", "value")},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := SentryHandler{attrs: tt.initialAttrs}
			newHandler := h.WithAttrs(tt.newAttrs)
			if !equalAttrs(newHandler.(*SentryHandler).attrs, tt.expected) {
				t.Errorf("WithAttrs() = %+v, want %+v", newHandler.(*SentryHandler).attrs, tt.expected)
			}
		})
	}
}

func TestSentryHandler_WithGroup(t *testing.T) {
	tests := map[string]struct {
		initialGroups []string
		newGroup      string
		expected      []string
	}{
		"Empty initial groups": {
			initialGroups: []string{},
			newGroup:      "group1",
			expected:      []string{"group1"},
		},
		"Non-empty initial groups": {
			initialGroups: []string{"existingGroup"},
			newGroup:      "newGroup",
			expected:      []string{"existingGroup", "newGroup"},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			h := SentryHandler{groups: tt.initialGroups}
			newHandler := h.WithGroup(tt.newGroup)
			if !equalStrings(newHandler.(*SentryHandler).groups, tt.expected) {
				t.Errorf("WithGroup() = %+v, want %+v", newHandler.(*SentryHandler).groups, tt.expected)
			}
		})
	}
}

func TestOption_NewSentryHandler(t *testing.T) {
	tests := map[string]struct {
		option   Option
		expected slog.Handler
	}{
		"Default options": {
			option:   Option{},
			expected: &SentryHandler{option: Option{Level: slog.LevelDebug, Converter: DefaultConverter, AttrFromContext: []func(ctx context.Context) []slog.Attr{}}},
		},
		"Custom options": {
			option: Option{
				Level:           slog.LevelInfo,
				Converter:       CustomConverter,
				AttrFromContext: []func(ctx context.Context) []slog.Attr{customAttrFromContext},
			},
			expected: &SentryHandler{
				option: Option{
					Level:           slog.LevelInfo,
					Converter:       CustomConverter,
					AttrFromContext: []func(ctx context.Context) []slog.Attr{customAttrFromContext},
				},
				attrs:  []slog.Attr{},
				groups: []string{},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.option.NewSentryHandler()
			if !compareHandlers(got, tt.expected) {
				t.Errorf("NewSentryHandler() = %+v, want %+v", got, tt.expected)
			}
		})
	}
}

func equalAttrs(a, b []slog.Attr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key || a[i].String() != b[i].String() {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compareHandlers(h1, h2 slog.Handler) bool {
	sh1, ok1 := h1.(*SentryHandler)
	sh2, ok2 := h2.(*SentryHandler)
	if !ok1 || !ok2 {
		return false
	}
	return sh1.option.Level == sh2.option.Level &&
		equalFuncs(sh1.option.AttrFromContext, sh2.option.AttrFromContext)
}

func equalFuncs(a, b []func(ctx context.Context) []slog.Attr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if fmt.Sprintf("%p", a[i]) != fmt.Sprintf("%p", b[i]) {
			return false
		}
	}
	return true
}

// Mock functions for custom converter and custom attr from context.
func CustomConverter(bool, func([]string, slog.Attr) slog.Attr, []slog.Attr, []string, *slog.Record, *sentry.Hub) *sentry.Event {
	return sentry.NewEvent()
}

func customAttrFromContext(context.Context) []slog.Attr {
	return []slog.Attr{slog.String("custom", "attr")}
}
