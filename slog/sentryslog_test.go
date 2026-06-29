package sentryslog

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
)

func TestSentryHandlerEnabled(t *testing.T) {
	tests := map[string]struct {
		logLevels  []slog.Level
		checkLevel slog.Level
		expected   bool
	}{
		"enabled": {
			logLevels:  []slog.Level{slog.LevelInfo, slog.LevelWarn},
			checkLevel: slog.LevelInfo,
			expected:   true,
		},
		"disabled": {
			logLevels:  []slog.Level{slog.LevelInfo, slog.LevelWarn},
			checkLevel: slog.LevelError,
			expected:   false,
		},
		"empty": {
			logLevels:  []slog.Level{},
			checkLevel: slog.LevelInfo,
			expected:   false,
		},
		"fatal": {
			logLevels:  []slog.Level{LevelFatal},
			checkLevel: LevelFatal,
			expected:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			option := Option{LogLevel: tt.logLevels}
			h := option.NewSentryHandler(context.Background())
			assert.Equal(t, tt.expected, h.Enabled(context.Background(), tt.checkLevel))
		})
	}
}

func TestSentryHandlerWithAttrs(t *testing.T) {
	option := Option{}
	h := option.NewSentryHandler(context.Background()).(*SentryHandler)
	h.logHandler.attrs = []slog.Attr{slog.String("existing", "attr")}

	newHandler := h.WithAttrs([]slog.Attr{slog.String("key", "value")}).(*SentryHandler)

	assert.True(t, equalAttrs(newHandler.logHandler.attrs, []slog.Attr{
		slog.String("existing", "attr"),
		slog.String("key", "value"),
	}))
}

func TestSentryHandlerWithGroup(t *testing.T) {
	option := Option{}
	h := option.NewSentryHandler(context.Background()).(*SentryHandler)
	h.logHandler.groups = []string{"existing"}

	newHandler := h.WithGroup("next").(*SentryHandler)

	assert.True(t, equalStrings(newHandler.logHandler.groups, []string{"existing", "next"}))
}

func TestOptionNewSentryHandler(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		handler := Option{}.NewSentryHandler(context.Background()).(*SentryHandler)

		assert.True(t, equalLevels(handler.logHandler.option.LogLevel, []slog.Level{
			slog.LevelDebug,
			slog.LevelInfo,
			slog.LevelWarn,
			slog.LevelError,
			LevelFatal,
		}))
		assert.Empty(t, handler.logHandler.option.AttrFromContext)
	})

	t.Run("custom", func(t *testing.T) {
		opt := Option{
			LogLevel:        []slog.Level{slog.LevelInfo, slog.LevelWarn},
			AttrFromContext: []func(ctx context.Context) []slog.Attr{customAttrFromContext},
		}
		handler := opt.NewSentryHandler(context.Background()).(*SentryHandler)

		assert.True(t, equalLevels(handler.logHandler.option.LogLevel, opt.LogLevel))
		assert.True(t, equalFuncs(handler.logHandler.option.AttrFromContext, opt.AttrFromContext))
	})
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

func equalFuncs(a, b []func(ctx context.Context) []slog.Attr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if (a[i] == nil) != (b[i] == nil) {
			return false
		}
		if a[i] == nil {
			continue
		}
		if fmt.Sprintf("%p", a[i]) != fmt.Sprintf("%p", b[i]) {
			return false
		}
	}
	return true
}

func equalLevels(a, b []slog.Level) bool {
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

func customAttrFromContext(context.Context) []slog.Attr {
	return []slog.Attr{slog.String("custom", "attr")}
}

func newMockTransport() (context.Context, *sentry.MockTransport) {
	ctx := context.Background()
	mockTransport := &sentry.MockTransport{}
	mockClient, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: mockTransport,
	})
	hub := sentry.CurrentHub()
	hub.BindClient(mockClient)
	ctx = sentry.SetHubOnContext(ctx, hub)
	return ctx, mockTransport
}

type mockLogValuer struct{}

func (t mockLogValuer) LogValue() slog.Value {
	return slog.StringValue("something")
}

func TestSentryHandlerAttrToSentryAttr(t *testing.T) {
	testCases := []struct {
		name          string
		attr          []any
		expectedKey   string
		expectedValue interface{}
	}{
		{name: "string", attr: []any{"key1", "value1"}, expectedKey: "key1", expectedValue: "value1"},
		{name: "int", attr: []any{"key2", 42}, expectedKey: "key2", expectedValue: int64(42)},
		{name: "bool", attr: []any{"key3", true}, expectedKey: "key3", expectedValue: true},
		{name: "float", attr: []any{"key4", 3.14}, expectedKey: "key4", expectedValue: 3.14},
		{name: "duration", attr: []any{"key5", 5 * time.Second}, expectedKey: "key5", expectedValue: "5s"},
		{name: "time", attr: []any{"key6", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)}, expectedKey: "key6", expectedValue: "2023-01-01T00:00:00Z"},
		{name: "uint64", attr: []any{"key8", uint64(math.MaxUint64)}, expectedKey: "key8", expectedValue: uint64(math.MaxUint64)},
		{name: "logvaluer", attr: []any{"key9", mockLogValuer{}}, expectedKey: "key9", expectedValue: "something"},
		{name: "struct", attr: []any{"key10", struct{ Name string }{"test"}}, expectedKey: "key10", expectedValue: "{Name:test}"},
		{name: "error", attr: []any{"key11", fmt.Errorf("error")}, expectedKey: "key11", expectedValue: "error"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				LogLevel: []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
			}.NewSentryHandler(ctx)
			logger := slog.New(handler)
			logger.InfoContext(ctx, "test message", tt.attr...)
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents))
			assert.Equal(t, "test message", gotEvents[0].Logs[0].Body)
			assert.Equal(t, sentry.LogLevelInfo, gotEvents[0].Logs[0].Level)

			value, found := gotEvents[0].Logs[0].Attributes[tt.expectedKey]
			assert.True(t, found)
			assert.Equal(t, tt.expectedValue, value.AsInterface())
			assert.Equal(t, "auto.log.slog", gotEvents[0].Logs[0].Attributes["sentry.origin"].AsInterface())
		})
	}
}

func TestSentryHandlerWithAttrsAndGroup(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	baseHandler := Option{
		LogLevel: []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
	}.NewSentryHandler(ctx)
	baseLogger := slog.New(baseHandler)

	nestedHandler := baseHandler.
		WithGroup("parent").
		WithAttrs([]slog.Attr{slog.String("parent_attr", "parent_value")}).
		WithGroup("child").
		WithAttrs([]slog.Attr{
			slog.String("child_attr", "child_value"),
			slog.Group("nested", slog.String("nested_attr", "nested_value")),
		})

	nestedLogger := slog.New(nestedHandler)
	nestedLogger.InfoContext(ctx, "test with nested groups and attrs", "direct_attr", "direct_value")
	baseLogger.InfoContext(ctx, "should not have attrs and groups")

	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))
	assert.Equal(t, "test with nested groups and attrs", gotEvents[0].Logs[0].Body)
	assert.Equal(t, "should not have attrs and groups", gotEvents[0].Logs[1].Body)

	parentAttr, found := gotEvents[0].Logs[0].Attributes["parent.parent_attr"]
	assert.True(t, found)
	assert.Equal(t, "parent_value", parentAttr.AsInterface())

	childAttr, found := gotEvents[0].Logs[0].Attributes["parent.child.child_attr"]
	assert.True(t, found)
	assert.Equal(t, "child_value", childAttr.AsInterface())

	nestedAttr, found := gotEvents[0].Logs[0].Attributes["parent.child.nested.nested_attr"]
	assert.True(t, found)
	assert.Equal(t, "nested_value", nestedAttr.AsInterface())

	directAttr, found := gotEvents[0].Logs[0].Attributes["parent.child.direct_attr"]
	assert.True(t, found)
	assert.Equal(t, "direct_value", directAttr.AsInterface())

	_, found = gotEvents[0].Logs[1].Attributes["parent.parent_attr"]
	assert.False(t, found)
}

func TestSentryHandlerLogLevels(t *testing.T) {
	testCases := []struct {
		name          string
		logFunc       func(ctx context.Context, logger *slog.Logger, msg string)
		message       string
		expectedLevel sentry.LogLevel
	}{
		{name: "debug", logFunc: func(ctx context.Context, logger *slog.Logger, msg string) { logger.DebugContext(ctx, msg) }, message: "debug message", expectedLevel: sentry.LogLevelDebug},
		{name: "info", logFunc: func(ctx context.Context, logger *slog.Logger, msg string) { logger.InfoContext(ctx, msg) }, message: "info message", expectedLevel: sentry.LogLevelInfo},
		{name: "warn", logFunc: func(ctx context.Context, logger *slog.Logger, msg string) { logger.WarnContext(ctx, msg) }, message: "warning message", expectedLevel: sentry.LogLevelWarn},
		{name: "error", logFunc: func(ctx context.Context, logger *slog.Logger, msg string) { logger.ErrorContext(ctx, msg) }, message: "error message", expectedLevel: sentry.LogLevelError},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				LogLevel: []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
			}.NewSentryHandler(ctx)
			logger := slog.New(handler)

			tt.logFunc(ctx, logger, tt.message)
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents))
			assert.Equal(t, tt.message, gotEvents[0].Logs[0].Body)
			assert.Equal(t, tt.expectedLevel, gotEvents[0].Logs[0].Level)
		})
	}
}

func TestSentryHandlerReplaceAttr(t *testing.T) {
	replaceAttr := func(_ []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, "replaced")
		}
		return a
	}

	ctx, mockTransport := newMockTransport()
	handler := Option{
		LogLevel:    []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		ReplaceAttr: replaceAttr,
	}.NewSentryHandler(ctx)

	logger := slog.New(handler)
	logger.InfoContext(ctx, "replace test", "foo", "bar", "num", 123)
	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	val, found := gotEvents[0].Logs[0].Attributes["foo"]
	assert.True(t, found)
	assert.Equal(t, "replaced", val.AsInterface())

	val, found = gotEvents[0].Logs[0].Attributes["num"]
	assert.True(t, found)
	assert.Equal(t, int64(123), val.AsInterface())
}

func TestSentryHandlerAddSource(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	handler := Option{
		LogLevel:  []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		AddSource: true,
	}.NewSentryHandler(ctx)

	logger := slog.New(handler)
	logger.InfoContext(ctx, "test with source")
	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	_, found := gotEvents[0].Logs[0].Attributes["source.line"]
	assert.True(t, found)
	_, found = gotEvents[0].Logs[0].Attributes["source.file"]
	assert.True(t, found)
	_, found = gotEvents[0].Logs[0].Attributes["source.function"]
	assert.True(t, found)
}

func TestSentryHandlerCustomLogLevels(t *testing.T) {
	testCases := []struct {
		name                string
		customLevel         slog.Level
		expectedSentryLevel sentry.LogLevel
	}{
		{name: "trace", customLevel: slog.Level(-8), expectedSentryLevel: sentry.LogLevelTrace},
		{name: "debug-range", customLevel: slog.Level(-2), expectedSentryLevel: sentry.LogLevelDebug},
		{name: "info-range", customLevel: slog.Level(2), expectedSentryLevel: sentry.LogLevelInfo},
		{name: "warn-range", customLevel: slog.Level(6), expectedSentryLevel: sentry.LogLevelWarn},
		{name: "error-range", customLevel: slog.Level(10), expectedSentryLevel: sentry.LogLevelError},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				LogLevel: []slog.Level{tt.customLevel},
			}.NewSentryHandler(ctx)
			logger := slog.New(handler)

			logger.LogAttrs(ctx, tt.customLevel, "test message with custom level", slog.String("level_name", tt.name))
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents))
			assert.Equal(t, "test message with custom level", gotEvents[0].Logs[0].Body)
			assert.Equal(t, tt.expectedSentryLevel, gotEvents[0].Logs[0].Level)

			levelName, found := gotEvents[0].Logs[0].Attributes["level_name"]
			assert.True(t, found)
			assert.Equal(t, tt.name, levelName.AsInterface())
		})
	}
}
