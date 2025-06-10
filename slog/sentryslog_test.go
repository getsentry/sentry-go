package sentryslog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
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

func newMockTransport() (context.Context, *sentry.MockTransport) {
	ctx := context.Background()
	mockTransport := &sentry.MockTransport{}
	mockClient, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:        "https://public@example.com/1",
		Transport:  mockTransport,
		EnableLogs: true,
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

func TestSentryHandler_AttrToSentryAttr(t *testing.T) {
	testCases := []struct {
		name          string
		attr          []any
		expectedKey   string
		expectedValue interface{}
	}{
		{
			name:          "String attribute",
			attr:          []any{"key1", "value1"},
			expectedKey:   "key1",
			expectedValue: "value1",
		},
		{
			name:          "Int attribute",
			attr:          []any{"key2", 42},
			expectedKey:   "key2",
			expectedValue: int64(42),
		},
		{
			name:          "Bool attribute",
			attr:          []any{"key3", true},
			expectedKey:   "key3",
			expectedValue: true,
		},
		{
			name:          "Float attribute",
			attr:          []any{"key4", 3.14},
			expectedKey:   "key4",
			expectedValue: 3.14,
		},
		{
			name:          "Duration attribute",
			attr:          []any{"key5", 5 * time.Second},
			expectedKey:   "key5",
			expectedValue: "5s",
		},
		{
			name:          "Time attribute",
			attr:          []any{"key6", time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
			expectedKey:   "key6",
			expectedValue: "2023-01-01T00:00:00Z",
		},
		{
			name:          "Uint64 attribute - convert to int64",
			attr:          []any{"key7", uint64(100)},
			expectedKey:   "key7",
			expectedValue: int64(100),
		},
		{
			name:          "Uint64 attribute - convert to float",
			attr:          []any{"key8", uint64(1 << 63)},
			expectedKey:   "key8",
			expectedValue: 9.223372036854776e+18,
		},
		{
			name:          "something attribute",
			attr:          []any{"key9", mockLogValuer{}},
			expectedKey:   "key9",
			expectedValue: "something",
		},
		{
			name:          "Any attribute (struct)",
			attr:          []any{"key10", struct{ Name string }{"test"}},
			expectedKey:   "key10",
			expectedValue: "{Name:test}",
		},
		{
			name:          "Error attribute",
			attr:          []any{"key11", errors.New("error")},
			expectedKey:   "key11",
			expectedValue: "error",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				CaptureType: LogType,
			}.NewSentryHandler()
			logger := slog.New(handler)
			logger.InfoContext(ctx, "test message", tt.attr...)
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents), "expected 1 event, got %d", len(gotEvents))
			assert.Equal(t, "test message", mockTransport.Events()[0].Logs[0].Body)
			assert.Equal(t, sentry.LogLevelInfo, mockTransport.Events()[0].Logs[0].Level)

			value, found := mockTransport.Events()[0].Logs[0].Attributes[tt.expectedKey]
			assert.True(t, found, "Attribute %s not found", tt.expectedKey)
			assert.Equal(t, tt.expectedValue, value.Value, "For %s, expected value %v, got %v", tt.expectedKey, tt.expectedValue, value)
		})
	}
}

func TestSentryHandler_WithAttrsAndGroup(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	baseHandler := Option{
		CaptureType: LogType,
	}.NewSentryHandler()
	baseLogger := slog.New(baseHandler)

	// Create a handler with nested groups and attributes
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

	// Check events
	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	// Verify nested group log attributes
	assert.Equal(t, "test with nested groups and attrs", mockTransport.Events()[0].Logs[0].Body)

	// Check parent level attribute
	parentAttr, found := mockTransport.Events()[0].Logs[0].Attributes["parent.parent_attr"]
	assert.True(t, found, "parent.parent_attr not found")
	assert.Equal(t, "parent_value", parentAttr.Value)

	// Check child level attribute
	childAttr, found := mockTransport.Events()[0].Logs[0].Attributes["parent.child.child_attr"]
	assert.True(t, found, "parent.child.child_attr not found")
	assert.Equal(t, "child_value", childAttr.Value)

	// Check nested group attribute
	nestedAttr, found := mockTransport.Events()[0].Logs[0].Attributes["parent.child.nested.nested_attr"]
	assert.True(t, found, "parent.child.nested.nested_attr not found")
	assert.Equal(t, "nested_value", nestedAttr.Value)

	// Check direct attribute
	directAttr, found := mockTransport.Events()[0].Logs[0].Attributes["parent.child.direct_attr"]
	assert.True(t, found, "parent.child.direct_attr not found")
	assert.Equal(t, "direct_value", directAttr.Value)

	// Verify base logger log doesn't have any of these attributes
	assert.Equal(t, "should not have attrs and groups", mockTransport.Events()[0].Logs[1].Body)

	_, found = mockTransport.Events()[0].Logs[1].Attributes["parent.parent_attr"]
	assert.False(t, found, "parent.parent_attr should be missing from base log")

	_, found = mockTransport.Events()[0].Logs[1].Attributes["parent.child.child_attr"]
	assert.False(t, found, "parent.child.child_attr should be missing from base log")
}

func TestSentryHandler_LogLevels(t *testing.T) {
	testCases := []struct {
		name          string
		logFunc       func(ctx context.Context, logger *slog.Logger, msg string)
		message       string
		expectedLevel sentry.LogLevel
	}{
		{
			name: "Debug level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.DebugContext(ctx, msg)
			},
			message:       "debug message",
			expectedLevel: sentry.LogLevelDebug,
		},
		{
			name: "Info level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.InfoContext(ctx, msg)
			},
			message:       "info message",
			expectedLevel: sentry.LogLevelInfo,
		},
		{
			name: "Warn level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.WarnContext(ctx, msg)
			},
			message:       "warning message",
			expectedLevel: sentry.LogLevelWarn,
		},
		{
			name: "Error level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.ErrorContext(ctx, msg)
			},
			message:       "error message",
			expectedLevel: sentry.LogLevelError,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				CaptureType: LogType,
				Level:       slog.LevelDebug,
			}.NewSentryHandler()
			logger := slog.New(handler)

			tt.logFunc(ctx, logger, tt.message)
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents), "expected 1 event, got %d", len(gotEvents))

			assert.Equal(t, tt.message, mockTransport.Events()[0].Logs[0].Body)
			assert.Equal(t, tt.expectedLevel, mockTransport.Events()[0].Logs[0].Level)
		})
	}
}

func TestSentryHandler_ReplaceAttr(t *testing.T) {
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, "replaced")
		}
		return a
	}
	ctx, mockTransport := newMockTransport()
	handler := Option{
		CaptureType: LogType,
		ReplaceAttr: replaceAttr,
		Level:       slog.LevelDebug,
	}.NewSentryHandler()

	logger := slog.New(handler)
	logger.InfoContext(ctx, "replace test", "foo", "bar", "num", 123)
	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))
	attrs := mockTransport.Events()[0].Logs[0].Attributes

	val, found := attrs["foo"]
	assert.True(t, found)
	assert.Equal(t, "replaced", val.Value)

	val, found = attrs["num"]
	assert.True(t, found)
	assert.Equal(t, int64(123), val.Value)
}

func TestSentryHandler_AddSource(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	handler := Option{
		CaptureType: LogType,
		AddSource:   true,
		Level:       slog.LevelDebug,
	}.NewSentryHandler()

	logger := slog.New(handler)
	logger.InfoContext(ctx, "test with source")
	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	// Check if source attribute exists
	_, found := mockTransport.Events()[0].Logs[0].Attributes["source.line"]
	assert.True(t, found, "source attribute not found")
	_, found = mockTransport.Events()[0].Logs[0].Attributes["source.file"]
	assert.True(t, found, "source attribute not found")
	_, found = mockTransport.Events()[0].Logs[0].Attributes["source.function"]
	assert.True(t, found, "source attribute not found")
}

func TestSentryHandler_EventType(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	handler := Option{
		CaptureType: EventType,
		Level:       slog.LevelDebug,
	}.NewSentryHandler()

	logger := slog.New(handler)
	message := fmt.Sprintf("%s message with event type", slog.LevelInfo)
	logger.InfoContext(ctx, message, "attr_key", "attr_value")
	sentry.Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	assert.Equal(t, 1, len(events), "should capture one event")

	assert.Nil(t, events[0].Logs, "should not have logs for EventType")
	assert.Equal(t, message, events[0].Message, "event message should match")

	_, found := events[0].Extra["attr_key"]
	assert.True(t, found, "attribute should be in event's Extra map")
}

func TestSentryHandler_EventTypeWithReplaceAttr(t *testing.T) {
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, "replaced_"+a.Value.String())
		}
		return a
	}

	ctx, mockTransport := newMockTransport()
	handler := Option{
		CaptureType: EventType,
		ReplaceAttr: replaceAttr,
		Level:       slog.LevelDebug,
	}.NewSentryHandler()

	logger := slog.New(handler)
	logger.InfoContext(ctx, "replace test event", "foo", "bar", "num", 123)
	sentry.Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	assert.Equal(t, 1, len(events))

	// Check if attribute was replaced
	fooAttr, found := events[0].Extra["foo"]
	assert.True(t, found)
	assert.Equal(t, "replaced_bar", fooAttr)

	// Check if non-string attribute is unchanged
	numAttr, found := events[0].Extra["num"]
	assert.True(t, found)
	assert.Equal(t, int64(123), numAttr)
}
