package sentry

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestSlogHandler(opts *slog.HandlerOptions) (slog.Handler, *MockTransport) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:        testDsn,
		Transport:  mockTransport,
		EnableLogs: true,
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx = SetHubOnContext(ctx, hub)

	return NewSentrySlogHandler(ctx, opts), mockTransport
}

type testLogValuer struct{}

func (t testLogValuer) LogValue() slog.Value {
	return slog.StringValue("test")
}

func TestSlogHandlerAttrToSentryAttr(t *testing.T) {
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
			name:          "LogValuer attribute",
			attr:          []any{"key9", testLogValuer{}},
			expectedKey:   "key9",
			expectedValue: "test",
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
			handler, mockTransport := newTestSlogHandler(nil)
			logger := slog.New(handler)
			logger.Info("test message", tt.attr...)
			Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents), "expected 1 event, got %d", len(gotEvents))
			assert.Equal(t, "test message", mockTransport.events[0].Logs[0].Body)
			assert.Equal(t, LogLevelInfo, mockTransport.events[0].Logs[0].Level)

			value, found := mockTransport.events[0].Logs[0].Attributes[tt.expectedKey]
			assert.True(t, found, "Attribute %s not found", tt.expectedKey)
			assert.Equal(t, tt.expectedValue, value.Value, "For %s, expected value %v, got %v", tt.expectedKey, tt.expectedValue, value)
		})
	}
}

func TestSlogHandlerWithAttrsAndGroup(t *testing.T) {
	baseHandler, mockTransport := newTestSlogHandler(nil)
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
	nestedLogger.Info("test with nested groups and attrs", "direct_attr", "direct_value")
	baseLogger.Info("should not have attrs and groups")

	Flush(20 * time.Millisecond)

	// Check events
	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))

	// Verify nested group log attributes
	assert.Equal(t, "test with nested groups and attrs", mockTransport.events[0].Logs[0].Body)

	// Check parent level attribute
	parentAttr, found := mockTransport.events[0].Logs[0].Attributes["parent.parent_attr"]
	assert.True(t, found, "parent.parent_attr not found")
	assert.Equal(t, "parent_value", parentAttr.Value)

	// Check child level attribute
	childAttr, found := mockTransport.events[0].Logs[0].Attributes["parent.child.child_attr"]
	assert.True(t, found, "parent.child.child_attr not found")
	assert.Equal(t, "child_value", childAttr.Value)

	// Check nested group attribute
	nestedAttr, found := mockTransport.events[0].Logs[0].Attributes["parent.child.nested.nested_attr"]
	assert.True(t, found, "parent.child.nested.nested_attr not found")
	assert.Equal(t, "nested_value", nestedAttr.Value)

	// Check direct attribute
	directAttr, found := mockTransport.events[0].Logs[0].Attributes["parent.child.direct_attr"]
	assert.True(t, found, "parent.child.direct_attr not found")
	assert.Equal(t, "direct_value", directAttr.Value)

	// Verify base logger log doesn't have any of these attributes
	assert.Equal(t, "should not have attrs and groups", mockTransport.events[0].Logs[1].Body)

	_, found = mockTransport.events[0].Logs[1].Attributes["parent.parent_attr"]
	assert.False(t, found, "parent.parent_attr should be missing from base log")

	_, found = mockTransport.events[0].Logs[1].Attributes["parent.child.child_attr"]
	assert.False(t, found, "parent.child.child_attr should be missing from base log")
}

func TestSlogHandlerLogLevels(t *testing.T) {
	testCases := []struct {
		name          string
		logFunc       func(logger *slog.Logger, msg string)
		message       string
		expectedLevel LogLevel
	}{
		{
			name: "Debug level",
			logFunc: func(logger *slog.Logger, msg string) {
				logger.Debug(msg)
			},
			message:       "debug message",
			expectedLevel: LogLevelDebug,
		},
		{
			name: "Info level",
			logFunc: func(logger *slog.Logger, msg string) {
				logger.Info(msg)
			},
			message:       "info message",
			expectedLevel: LogLevelInfo,
		},
		{
			name: "Warn level",
			logFunc: func(logger *slog.Logger, msg string) {
				logger.Warn(msg)
			},
			message:       "warning message",
			expectedLevel: LogLevelWarn,
		},
		{
			name: "Error level",
			logFunc: func(logger *slog.Logger, msg string) {
				logger.Error(msg)
			},
			message:       "error message",
			expectedLevel: LogLevelError,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			handler, mockTransport := newTestSlogHandler(&slog.HandlerOptions{Level: slog.LevelDebug})
			logger := slog.New(handler)

			tt.logFunc(logger, tt.message)
			Flush(20 * time.Millisecond)

			gotEvents := mockTransport.Events()
			assert.Equal(t, 1, len(gotEvents), "expected 1 event, got %d", len(gotEvents))

			assert.Equal(t, tt.message, mockTransport.events[0].Logs[0].Body)
			assert.Equal(t, tt.expectedLevel, mockTransport.events[0].Logs[0].Level)
		})
	}
}

func TestSlogHandlerReplaceAttr(t *testing.T) {
	replaceAttr := func(groups []string, a slog.Attr) slog.Attr {
		if a.Value.Kind() == slog.KindString {
			return slog.String(a.Key, "replaced")
		}
		return a
	}
	handler, mockTransport := newTestSlogHandler(&slog.HandlerOptions{
		Level:       slog.LevelDebug,
		ReplaceAttr: replaceAttr,
	})
	logger := slog.New(handler)
	logger.Info("replace test", "foo", "bar", "num", 123)
	Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	assert.Equal(t, 1, len(gotEvents))
	attrs := mockTransport.events[0].Logs[0].Attributes

	val, found := attrs["foo"]
	assert.True(t, found)
	assert.Equal(t, "replaced", val.Value)

	val, found = attrs["num"]
	assert.True(t, found)
	assert.Equal(t, int64(123), val.Value)
}
