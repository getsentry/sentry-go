package sentryzap

import (
	"context"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

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

func TestSentryCore_Enabled(t *testing.T) {
	tests := map[string]struct {
		logLevels  []zapcore.Level
		checkLevel zapcore.Level
		expected   bool
	}{
		"Debug enabled": {
			logLevels:  []zapcore.Level{zapcore.DebugLevel, zapcore.InfoLevel},
			checkLevel: zapcore.DebugLevel,
			expected:   true,
		},
		"Debug not enabled": {
			logLevels:  []zapcore.Level{zapcore.InfoLevel, zapcore.WarnLevel},
			checkLevel: zapcore.DebugLevel,
			expected:   false,
		},
		"Error in all levels": {
			logLevels:  []zapcore.Level{zapcore.DebugLevel, zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel},
			checkLevel: zapcore.ErrorLevel,
			expected:   true,
		},
		"Empty levels": {
			logLevels:  []zapcore.Level{},
			checkLevel: zapcore.InfoLevel,
			expected:   false,
		},
		"Only fatal": {
			logLevels:  []zapcore.Level{zapcore.FatalLevel},
			checkLevel: zapcore.FatalLevel,
			expected:   true,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, _ := newMockTransport()
			core := NewSentryCore(ctx, Option{Level: tt.logLevels})
			assert.Equal(t, tt.expected, core.Enabled(tt.checkLevel))
		})
	}
}

func TestSentryCore_DefaultOptions(t *testing.T) {
	ctx, _ := newMockTransport()
	core := NewSentryCore(ctx, Option{})

	assert.True(t, core.Enabled(zapcore.DebugLevel))
	assert.True(t, core.Enabled(zapcore.InfoLevel))
	assert.True(t, core.Enabled(zapcore.WarnLevel))
	assert.True(t, core.Enabled(zapcore.ErrorLevel))
	assert.True(t, core.Enabled(zapcore.DPanicLevel))
	assert.True(t, core.Enabled(zapcore.PanicLevel))
	assert.True(t, core.Enabled(zapcore.FatalLevel))

	assert.Equal(t, 5*time.Second, core.option.FlushTimeout)
}

func TestSentryCore_With(t *testing.T) {
	ctx, _ := newMockTransport()
	core := NewSentryCore(ctx, Option{})

	newCore := core.With([]zapcore.Field{
		zap.String("service", "test-service"),
		zap.Int("version", 1),
	})

	sentryCore := newCore.(*SentryCore)
	assert.Len(t, sentryCore.fields, 2)
	assert.Equal(t, "service", sentryCore.fields[0].Key)
	assert.Equal(t, "version", sentryCore.fields[1].Key)

	assert.Len(t, core.fields, 0)
}

func TestSentryCore_WithChained(t *testing.T) {
	ctx, _ := newMockTransport()
	core := NewSentryCore(ctx, Option{})

	newCore := core.
		With([]zapcore.Field{zap.String("field1", "value1")}).
		With([]zapcore.Field{zap.String("field2", "value2")}).
		With([]zapcore.Field{zap.String("field3", "value3")})

	sentryCore := newCore.(*SentryCore)
	assert.Len(t, sentryCore.fields, 3)
}

func TestSentryCore_Check(t *testing.T) {
	ctx, _ := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel, zapcore.ErrorLevel},
	})

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "test message",
	}

	ce := &zapcore.CheckedEntry{}
	result := core.Check(entry, ce)
	assert.NotNil(t, result)

	entry.Level = zapcore.DebugLevel
	ce = &zapcore.CheckedEntry{}
	result = core.Check(entry, ce)
	assert.Equal(t, ce, result)
}

func TestSentryCore_Write(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "test log message",
		Time:    time.Now(),
	}

	fields := []zapcore.Field{
		zap.String("key", "value"),
		zap.Int("count", 42),
	}

	err := core.Write(entry, fields)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]
	assert.Equal(t, "test log message", log.Body)
	assert.Equal(t, sentry.LogLevelInfo, log.Level)

	keyAttr, found := log.Attributes["key"]
	assert.True(t, found)
	assert.Equal(t, "value", keyAttr.Value)

	countAttr, found := log.Attributes["count"]
	assert.True(t, found)
	assert.Equal(t, int64(42), countAttr.Value)
}

func TestSentryCore_WriteWithAccumulatedFields(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	coreWithFields := core.With([]zapcore.Field{
		zap.String("service", "my-service"),
	})

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "test with accumulated fields",
		Time:    time.Now(),
	}

	additionalFields := []zapcore.Field{
		zap.String("request_id", "123"),
	}

	err := coreWithFields.Write(entry, additionalFields)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]

	serviceAttr, found := log.Attributes["service"]
	assert.True(t, found)
	assert.Equal(t, "my-service", serviceAttr.Value)

	reqIdAttr, found := log.Attributes["request_id"]
	assert.True(t, found)
	assert.Equal(t, "123", reqIdAttr.Value)
}

func TestSentryCore_WriteWithCaller(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level:     []zapcore.Level{zapcore.InfoLevel},
		AddCaller: true,
	})

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "test with caller",
		Time:    time.Now(),
		Caller: zapcore.EntryCaller{
			Defined:  true,
			File:     "/path/to/file.go",
			Line:     42,
			Function: "myFunction",
		},
	}

	err := core.Write(entry, nil)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]

	fileAttr, found := log.Attributes["code.filepath"]
	assert.True(t, found)
	assert.Equal(t, "/path/to/file.go", fileAttr.Value)

	lineAttr, found := log.Attributes["code.lineno"]
	assert.True(t, found)
	assert.Equal(t, int64(42), lineAttr.Value)

	funcAttr, found := log.Attributes["code.function"]
	assert.True(t, found)
	assert.Equal(t, "myFunction", funcAttr.Value)
}

func TestSentryCore_WriteWithLoggerName(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	entry := zapcore.Entry{
		Level:      zapcore.InfoLevel,
		Message:    "test with logger name",
		LoggerName: "my.logger.name",
		Time:       time.Now(),
	}

	err := core.Write(entry, nil)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]

	nameAttr, found := log.Attributes["logger.name"]
	assert.True(t, found)
	assert.Equal(t, "my.logger.name", nameAttr.Value)
}

func TestSentryCore_WriteWithStack(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.ErrorLevel},
	})

	entry := zapcore.Entry{
		Level:   zapcore.ErrorLevel,
		Message: "error with stack",
		Time:    time.Now(),
		Stack:   "goroutine 1 [running]:\nmain.main()\n\t/path/to/main.go:10",
	}

	err := core.Write(entry, nil)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]

	stackAttr, found := log.Attributes["exception.stacktrace"]
	assert.True(t, found)
	assert.Contains(t, stackAttr.Value, "goroutine 1")
}

func TestSentryCore_LogLevels(t *testing.T) {
	tests := []struct {
		name          string
		zapLevel      zapcore.Level
		expectedLevel sentry.LogLevel
	}{
		{
			name:          "Debug level",
			zapLevel:      zapcore.DebugLevel,
			expectedLevel: sentry.LogLevelDebug,
		},
		{
			name:          "Info level",
			zapLevel:      zapcore.InfoLevel,
			expectedLevel: sentry.LogLevelInfo,
		},
		{
			name:          "Warn level",
			zapLevel:      zapcore.WarnLevel,
			expectedLevel: sentry.LogLevelWarn,
		},
		{
			name:          "Error level",
			zapLevel:      zapcore.ErrorLevel,
			expectedLevel: sentry.LogLevelError,
		},
		{
			name:          "DPanic level",
			zapLevel:      zapcore.DPanicLevel,
			expectedLevel: sentry.LogLevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := newMockTransport()
			core := NewSentryCore(ctx, Option{
				Level: []zapcore.Level{tt.zapLevel},
			})

			entry := zapcore.Entry{
				Level:   tt.zapLevel,
				Message: "test message",
				Time:    time.Now(),
			}

			err := core.Write(entry, nil)
			assert.NoError(t, err)

			sentry.Flush(testutils.FlushTimeout())

			events := mockTransport.Events()
			require.Equal(t, 1, len(events))
			require.NotEmpty(t, events[0].Logs)

			assert.Equal(t, tt.expectedLevel, events[0].Logs[0].Level)
		})
	}
}

func TestSentryCore_Origin(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	entry := zapcore.Entry{
		Level:   zapcore.InfoLevel,
		Message: "test origin",
		Time:    time.Now(),
	}

	err := core.Write(entry, nil)
	assert.NoError(t, err)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	originAttr, found := events[0].Logs[0].Attributes["sentry.origin"]
	assert.True(t, found)
	assert.Equal(t, ZapOrigin, originAttr.Value)
}

func TestSentryCore_Integration(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	sentryCore := NewSentryCore(ctx, Option{
		Level:     []zapcore.Level{zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel},
		AddCaller: true,
	})

	logger := zap.New(sentryCore)
	logger.Info("user logged in", zap.String("user_id", "123"), zap.String("ip", "192.168.1.1"))
	logger.Warn("high memory usage", zap.Float64("usage_percent", 85.5))

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.Equal(t, 2, len(events[0].Logs))

	log1 := events[0].Logs[0]
	assert.Equal(t, "user logged in", log1.Body)
	assert.Equal(t, sentry.LogLevelInfo, log1.Level)

	userIdAttr, found := log1.Attributes["user_id"]
	assert.True(t, found)
	assert.Equal(t, "123", userIdAttr.Value)

	// Check second log
	log2 := events[0].Logs[1]
	assert.Equal(t, "high memory usage", log2.Body)
	assert.Equal(t, sentry.LogLevelWarn, log2.Level)

	usageAttr, found := log2.Attributes["usage_percent"]
	assert.True(t, found)
	assert.Equal(t, 85.5, usageAttr.Value)
}

func TestSentryCore_IntegrationWithTee(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	sentryCore := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.WarnLevel, zapcore.ErrorLevel},
	})

	// Create a tee with both a noop core and sentry core
	// This simulates the common pattern of logging to console AND sentry
	combinedCore := zapcore.NewTee(
		zapcore.NewNopCore(),
		sentryCore,
	)

	logger := zap.New(combinedCore)
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warning message")
	logger.Error("error message")

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.Equal(t, 2, len(events[0].Logs))

	assert.Equal(t, "warning message", events[0].Logs[0].Body)
	assert.Equal(t, "error message", events[0].Logs[1].Body)
}

func TestSentryCore_IntegrationWithSugaredLogger(t *testing.T) {
	ctx, mockTransport := newMockTransport()
	sentryCore := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	logger := zap.New(sentryCore).Sugar()
	logger.Infow("request processed",
		"method", "GET",
		"path", "/api/users",
		"status", 200,
	)

	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]
	assert.Equal(t, "request processed", log.Body)

	methodAttr, found := log.Attributes["method"]
	assert.True(t, found)
	assert.Equal(t, "GET", methodAttr.Value)

	statusAttr, found := log.Attributes["status"]
	assert.True(t, found)
	assert.Equal(t, int64(200), statusAttr.Value)
}

func TestSentryCore_Sync(t *testing.T) {
	ctx, _ := newMockTransport()
	core := NewSentryCore(ctx, Option{
		FlushTimeout: 50 * time.Millisecond,
	})

	err := core.Sync()
	assert.NoError(t, err)
}

func TestSentryCore_ContextField(t *testing.T) {
	ctx, _ := newMockTransport()

	span := sentry.StartSpan(ctx, "test.operation")
	newCtx := span.Context()
	defer span.Finish()

	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})
	coreWithCtx := core.With([]zapcore.Field{
		Context(newCtx),
	})
	sentryCore := coreWithCtx.(*SentryCore)

	assert.Equal(t, newCtx, sentryCore.ctx)
	assert.Len(t, sentryCore.fields, 0)
}

func TestSentryCore_ContextFieldWithOtherFields(t *testing.T) {
	ctx, _ := newMockTransport()
	span := sentry.StartSpan(ctx, "test.operation")
	newCtx := span.Context()
	defer span.Finish()

	core := NewSentryCore(ctx, Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})

	coreWithCtx := core.With([]zapcore.Field{
		zap.String("service", "test-service"),
		Context(newCtx),
		zap.Int("version", 1),
	})
	sentryCore := coreWithCtx.(*SentryCore)

	assert.Equal(t, newCtx, sentryCore.ctx)
	assert.Len(t, sentryCore.fields, 2)
	assert.Equal(t, "service", sentryCore.fields[0].Key)
	assert.Equal(t, "version", sentryCore.fields[1].Key)
}

func TestSentryCore_ContextFieldInLogger(t *testing.T) {
	ctx, mockTransport := newMockTransport()

	span := sentry.StartTransaction(ctx, "test-transaction")
	txnCtx := span.Context()
	defer span.Finish()

	logger := zap.New(NewSentryCore(context.Background(), Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	}))
	logger = logger.With(Context(txnCtx))
	logger.Info("test message with trace context", zap.String("key", "value"))
	sentry.Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	require.Equal(t, 1, len(events))
	require.NotEmpty(t, events[0].Logs)

	log := events[0].Logs[0]
	assert.Equal(t, "test message with trace context", log.Body)

	keyAttr, found := log.Attributes["key"]
	assert.True(t, found)
	assert.Equal(t, "value", keyAttr.Value)
	assert.Equal(t, span.TraceID, events[0].Logs[0].TraceID)
	assert.Equal(t, span.SpanID, events[0].Logs[0].SpanID)
}
