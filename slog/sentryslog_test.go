package sentryslog

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSentryHandler_Enabled(t *testing.T) {
	tests := map[string]struct {
		eventLevels []slog.Level
		logLevels   []slog.Level
		checkLevel  slog.Level
		expected    bool
	}{
		"Specific levels: Info in log levels only": {
			eventLevels: []slog.Level{slog.LevelError, LevelFatal},
			logLevels:   []slog.Level{slog.LevelInfo, slog.LevelWarn},
			checkLevel:  slog.LevelInfo,
			expected:    true, // Info is in log levels
		},
		"Specific levels: Debug not in any levels": {
			eventLevels: []slog.Level{slog.LevelError, LevelFatal},
			logLevels:   []slog.Level{slog.LevelInfo, slog.LevelWarn},
			checkLevel:  slog.LevelDebug,
			expected:    false, // Debug is not in either slice
		},
		"Specific levels: Error in both levels": {
			eventLevels: []slog.Level{slog.LevelError, LevelFatal},
			logLevels:   []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError},
			checkLevel:  slog.LevelError,
			expected:    true, // Error is in both slices
		},
		"Empty event levels, Info in log levels": {
			eventLevels: []slog.Level{},
			logLevels:   []slog.Level{slog.LevelDebug, slog.LevelInfo},
			checkLevel:  slog.LevelInfo,
			expected:    true, // Info is in log levels
		},
		"Empty log levels, Error in event levels": {
			eventLevels: []slog.Level{slog.LevelError, LevelFatal},
			logLevels:   []slog.Level{},
			checkLevel:  slog.LevelError,
			expected:    true, // Error is in event levels
		},
		"Both empty slices": {
			eventLevels: []slog.Level{},
			logLevels:   []slog.Level{},
			checkLevel:  slog.LevelInfo,
			expected:    false, // No levels enabled
		},
		"Only Fatal level enabled": {
			eventLevels: []slog.Level{LevelFatal},
			logLevels:   []slog.Level{},
			checkLevel:  LevelFatal,
			expected:    true, // Fatal is enabled
		},
		"Only Fatal level enabled, check Error": {
			eventLevels: []slog.Level{LevelFatal},
			logLevels:   []slog.Level{},
			checkLevel:  slog.LevelError,
			expected:    false, // Error is not enabled, only Fatal
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			option := Option{EventLevel: tt.eventLevels, LogLevel: tt.logLevels}
			h := option.NewSentryHandler(context.Background())
			if got := h.Enabled(context.Background(), tt.checkLevel); got != tt.expected {
				t.Errorf("Enabled() = %v, want %v (EventLevel: %v, LogLevel: %v, CheckLevel: %v)", got, tt.expected, tt.eventLevels, tt.logLevels, tt.checkLevel)
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
			option := Option{}
			h := option.NewSentryHandler(context.Background()).(*SentryHandler)

			h.eventHandler.attrs = tt.initialAttrs
			h.logHandler.attrs = tt.initialAttrs

			newHandler := h.WithAttrs(tt.newAttrs)
			sh := newHandler.(*SentryHandler)
			if !equalAttrs(sh.eventHandler.attrs, tt.expected) {
				t.Errorf("eventHandler WithAttrs() = %+v, want %+v", sh.eventHandler.attrs, tt.expected)
			}
			if !equalAttrs(sh.logHandler.attrs, tt.expected) {
				t.Errorf("logHandler WithAttrs() = %+v, want %+v", sh.logHandler.attrs, tt.expected)
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
			option := Option{}
			h := option.NewSentryHandler(context.Background()).(*SentryHandler)
			h.eventHandler.groups = tt.initialGroups
			h.logHandler.groups = tt.initialGroups
			newHandler := h.WithGroup(tt.newGroup)
			sh := newHandler.(*SentryHandler)
			if !equalStrings(sh.eventHandler.groups, tt.expected) {
				t.Errorf("eventHandler WithGroup() = %+v, want %+v", sh.eventHandler.groups, tt.expected)
			}
			if !equalStrings(sh.logHandler.groups, tt.expected) {
				t.Errorf("logHandler WithGroup() = %+v, want %+v", sh.logHandler.groups, tt.expected)
			}
		})
	}
}

func TestOption_NewSentryHandler(t *testing.T) {
	tests := map[string]struct {
		option   Option
		expected Option
	}{
		"Default options": {
			option: Option{},
			expected: Option{
				EventLevel:      []slog.Level{slog.LevelError, LevelFatal},
				LogLevel:        []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
				Converter:       DefaultConverter,
				AttrFromContext: []func(ctx context.Context) []slog.Attr{}},
		},
		"Custom options": {
			option: Option{
				EventLevel:      []slog.Level{slog.LevelWarn, slog.LevelError},
				LogLevel:        []slog.Level{slog.LevelInfo, slog.LevelWarn},
				Converter:       CustomConverter,
				AttrFromContext: []func(ctx context.Context) []slog.Attr{customAttrFromContext},
			},
			expected: Option{
				EventLevel:      []slog.Level{slog.LevelWarn, slog.LevelError},
				LogLevel:        []slog.Level{slog.LevelInfo, slog.LevelWarn},
				Converter:       CustomConverter,
				AttrFromContext: []func(ctx context.Context) []slog.Attr{customAttrFromContext},
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.option.NewSentryHandler(context.Background())
			sh := got.(*SentryHandler)

			if !equalLevels(sh.eventHandler.option.EventLevel, tt.expected.EventLevel) {
				t.Errorf("eventHandler EventLevel = %v, want %v", sh.eventHandler.option.EventLevel, tt.expected.EventLevel)
			}
			if !equalLevels(sh.logHandler.option.LogLevel, tt.expected.LogLevel) {
				t.Errorf("logHandler LogLevel = %v, want %v", sh.logHandler.option.LogLevel, tt.expected.LogLevel)
			}
			if !equalFuncs(sh.eventHandler.option.AttrFromContext, tt.expected.AttrFromContext) {
				t.Errorf("eventHandler AttrFromContext functions don't match")
			}
			if !equalFuncs(sh.logHandler.option.AttrFromContext, tt.expected.AttrFromContext) {
				t.Errorf("logHandler AttrFromContext functions don't match")
			}
		})
	}
}

// Test backwards compatibility with the deprecated Level field
func TestOption_NewSentryHandler_BackwardsCompatibility(t *testing.T) {
	tests := map[string]struct {
		option        Option
		expectedEvent []slog.Level
		expectedLog   []slog.Level
	}{
		"Level set to Info": {
			option: Option{
				Level: slog.LevelInfo,
			},
			expectedEvent: []slog.Level{slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
			expectedLog:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		},
		"Level set to Error": {
			option: Option{
				Level: slog.LevelError,
			},
			expectedEvent: []slog.Level{slog.LevelError, LevelFatal},
			expectedLog:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		},
		"Level set to Debug": {
			option: Option{
				Level: slog.LevelDebug,
			},
			expectedEvent: []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
			expectedLog:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		},
		"EventLevel takes precedence over Level": {
			option: Option{
				Level:      slog.LevelError,
				EventLevel: []slog.Level{slog.LevelWarn},
			},
			expectedEvent: []slog.Level{slog.LevelWarn},
			expectedLog:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.option.NewSentryHandler(context.Background())
			sh := got.(*SentryHandler)

			if !equalLevels(sh.eventHandler.option.EventLevel, tt.expectedEvent) {
				t.Errorf("eventHandler EventLevel = %v, want %v", sh.eventHandler.option.EventLevel, tt.expectedEvent)
			}
			if !equalLevels(sh.logHandler.option.LogLevel, tt.expectedLog) {
				t.Errorf("logHandler LogLevel = %v, want %v", sh.logHandler.option.LogLevel, tt.expectedLog)
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
			attr:          []any{"key8", uint64(math.MaxUint64)},
			expectedKey:   "key8",
			expectedValue: strconv.FormatUint(math.MaxUint64, 10),
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
				LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}, // Capture logs
				EventLevel: []slog.Level{},                                                                             // Do not capture events for this test
			}.NewSentryHandler(ctx)
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
			assert.Equal(t, "auto.logger.slog", mockTransport.Events()[0].Logs[0].Attributes["sentry.origin"].Value, "incorrect sentry.origin")
		})
	}
}

func TestSentryHandler_WithAttrsAndGroup(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	baseHandler := Option{
		LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		EventLevel: []slog.Level{},
	}.NewSentryHandler(ctx)
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
		slogLevel     slog.Level
		message       string
		expectedLevel sentry.LogLevel
	}{
		{
			name: "Debug level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.DebugContext(ctx, msg)
			},
			slogLevel:     slog.LevelDebug,
			message:       "debug message",
			expectedLevel: sentry.LogLevelDebug,
		},
		{
			name: "Info level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.InfoContext(ctx, msg)
			},
			slogLevel:     slog.LevelInfo,
			message:       "info message",
			expectedLevel: sentry.LogLevelInfo,
		},
		{
			name: "Warn level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.WarnContext(ctx, msg)
			},
			slogLevel:     slog.LevelWarn,
			message:       "warning message",
			expectedLevel: sentry.LogLevelWarn,
		},
		{
			name: "Error level",
			logFunc: func(ctx context.Context, logger *slog.Logger, msg string) {
				logger.ErrorContext(ctx, msg)
			},
			slogLevel:     slog.LevelError,
			message:       "error message",
			expectedLevel: sentry.LogLevelError,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
				EventLevel: []slog.Level{},
			}.NewSentryHandler(ctx)
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
		LogLevel:    []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}, // Capture as log
		EventLevel:  []slog.Level{},                                                                             // Don't capture as event
		ReplaceAttr: replaceAttr,
	}.NewSentryHandler(ctx)

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
		EventLevel: []slog.Level{},
		LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal},
		AddSource:  true,
	}.NewSentryHandler(ctx)

	logger := slog.New(handler)
	logger.InfoContext(ctx, "test with source")
	sentry.Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	// Check if source attribute exists
	_, found := gotEvents[0].Logs[0].Attributes["source.line"]
	assert.True(t, found, "source attribute not found")
	_, found = gotEvents[0].Logs[0].Attributes["source.file"]
	assert.True(t, found, "source attribute not found")
	_, found = gotEvents[0].Logs[0].Attributes["source.function"]
	assert.True(t, found, "source attribute not found")
}

func TestSentryHandler_EventType(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	handler := Option{
		EventLevel: []slog.Level{slog.LevelInfo}, // Changed to capture Info level as event
		LogLevel:   []slog.Level{},               // No log capture for this test
	}.NewSentryHandler(ctx)

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
		EventLevel:  []slog.Level{slog.LevelInfo}, // Changed to capture Info level as event
		LogLevel:    []slog.Level{},               // No log capture for this test
		ReplaceAttr: replaceAttr,
	}.NewSentryHandler(ctx)

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

func TestSentryHandler_CaptureAsEventAndLog(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	handler := Option{
		EventLevel: []slog.Level{slog.LevelWarn}, // Capture Warn as event
		LogLevel:   []slog.Level{slog.LevelWarn}, // Also capture Warn as log
	}.NewSentryHandler(ctx)

	logger := slog.New(handler)
	message := "warning message for both Event and Log"
	logger.WarnContext(ctx, message, "common_attr", "common_value")
	sentry.Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	require.Equal(t, 2, len(events), "should capture two events")
	event := events[0]

	assert.Equal(t, message, event.Message, "event message should match")
	assert.Equal(t, sentry.LevelWarning, event.Level)
	eventAttrVal, found := event.Extra["common_attr"]
	assert.True(t, found, "attribute should be in event's Extra map")
	assert.Equal(t, "common_value", eventAttrVal)

	event = events[1]
	require.NotEmpty(t, event.Logs, "should have Sentry log entries")
	logEntry := event.Logs[0]
	assert.Equal(t, message, logEntry.Body)
	assert.Equal(t, sentry.LogLevelWarn, logEntry.Level)
	logAttrVal, found := logEntry.Attributes["common_attr"]
	assert.True(t, found, "attribute should be in log entry's attributes")
	assert.Equal(t, sentry.Attribute{Value: "common_value", Type: "string"}, logAttrVal)
}

func TestSentryHandler_CustomLogLevels(t *testing.T) {
	testCases := []struct {
		name                string
		customLevel         slog.Level
		expectedSentryLevel sentry.LogLevel
		description         string
	}{
		{
			name:                "Trace level",
			customLevel:         slog.Level(-8),
			expectedSentryLevel: sentry.LogLevelTrace,
			description:         "Custom level below Debug should use Trace",
		},
		{
			name:                "Between Debug and Info",
			customLevel:         slog.Level(-2),
			expectedSentryLevel: sentry.LogLevelDebug,
			description:         "Custom level between Debug and Info should use Debug",
		},
		{
			name:                "Between Info and Warn",
			customLevel:         slog.Level(2),
			expectedSentryLevel: sentry.LogLevelInfo,
			description:         "Custom level between Info and Warn should use Info",
		},
		{
			name:                "Between Warn and Error",
			customLevel:         slog.Level(6),
			expectedSentryLevel: sentry.LogLevelWarn,
			description:         "Custom level between Warn and Error should use Warn",
		},
		{
			name:                "Between Error and Fatal",
			customLevel:         slog.Level(10),
			expectedSentryLevel: sentry.LogLevelError,
			description:         "Custom level between Error and Fatal should use Error",
		},
		// Note: We skip testing Fatal level (12+) because Fatal calls os.Exit(1)
		// which would terminate the test process. The logic is covered by the
		// range-based implementation in the actual handler.
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			handler := Option{
				LogLevel:   []slog.Level{tt.customLevel}, // Capture the specific custom level
				EventLevel: []slog.Level{},
			}.NewSentryHandler(ctx)
			logger := slog.New(handler)

			logger.LogAttrs(ctx, tt.customLevel, "test message with custom level", slog.String("level_name", tt.name))
			sentry.Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents), "expected 1 event, got %d for %s", len(gotEvents), tt.description)

			assert.Equal(t, "test message with custom level", mockTransport.Events()[0].Logs[0].Body)
			assert.Equal(t, tt.expectedSentryLevel, mockTransport.Events()[0].Logs[0].Level,
				"For %s: expected level %v, got %v", tt.description, tt.expectedSentryLevel, mockTransport.Events()[0].Logs[0].Level)

			levelName, found := mockTransport.Events()[0].Logs[0].Attributes["level_name"]
			assert.True(t, found, "level_name attribute not found for %s", tt.description)
			assert.Equal(t, tt.name, levelName.Value, "level_name value mismatch for %s", tt.description)
		})
	}
}

// Test that Enabled returns false for levels not in the slices
func TestSentryHandler_EnabledSpecificLevels(t *testing.T) {
	tests := map[string]struct {
		eventLevels []slog.Level
		logLevels   []slog.Level
		testLevel   slog.Level
		expected    bool
	}{
		"Level in event slice": {
			eventLevels: []slog.Level{slog.LevelWarn, slog.LevelError},
			logLevels:   []slog.Level{slog.LevelDebug},
			testLevel:   slog.LevelWarn,
			expected:    true,
		},
		"Level in log slice": {
			eventLevels: []slog.Level{slog.LevelError},
			logLevels:   []slog.Level{slog.LevelDebug, slog.LevelInfo},
			testLevel:   slog.LevelInfo,
			expected:    true,
		},
		"Level in both slices": {
			eventLevels: []slog.Level{slog.LevelWarn, slog.LevelError},
			logLevels:   []slog.Level{slog.LevelWarn, slog.LevelInfo},
			testLevel:   slog.LevelWarn,
			expected:    true,
		},
		"Level not in any slice": {
			eventLevels: []slog.Level{slog.LevelError},
			logLevels:   []slog.Level{slog.LevelDebug},
			testLevel:   slog.LevelInfo,
			expected:    false,
		},
		"Empty slices": {
			eventLevels: []slog.Level{},
			logLevels:   []slog.Level{},
			testLevel:   slog.LevelInfo,
			expected:    false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			option := Option{
				EventLevel: tt.eventLevels,
				LogLevel:   tt.logLevels,
			}
			handler := option.NewSentryHandler(context.Background())

			result := handler.Enabled(context.Background(), tt.testLevel)
			assert.Equal(t, tt.expected, result,
				"Expected Enabled(%v) = %v with EventLevel=%v, LogLevel=%v",
				tt.testLevel, tt.expected, tt.eventLevels, tt.logLevels)
		})
	}
}
