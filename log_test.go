package sentry

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
)

const (
	LogTraceID = "d49d9bf66f13450b81f65bc51cf49c03"
)

func setupMockTransport() (context.Context, *MockTransport) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableLogs:    true,
		EnableTracing: true,
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	ctx = SetHubOnContext(ctx, hub)
	return ctx, mockTransport
}

func Test_sentryLogger_MethodsWithFormat(t *testing.T) {
	attrs := map[string]Attribute{
		"sentry.release":              {Value: "v1.2.3", Type: "string"},
		"sentry.environment":          {Value: "testing", Type: "string"},
		"sentry.server.address":       {Value: "test-server", Type: "string"},
		"sentry.sdk.name":             {Value: "sentry.go", Type: "string"},
		"sentry.sdk.version":          {Value: "0.10.0", Type: "string"},
		"sentry.message.template":     {Value: "param matching: %v and %v", Type: "string"},
		"sentry.message.parameters.0": {Value: "param1", Type: "string"},
		"sentry.message.parameters.1": {Value: "param2", Type: "string"},
		"key.int":                     {Value: int64(42), Type: "integer"},
		"key.float":                   {Value: 42.2, Type: "double"},
		"key.bool":                    {Value: true, Type: "boolean"},
		"key.string":                  {Value: "str", Type: "string"},
	}

	tests := []struct {
		name       string
		logFunc    func(ctx context.Context, l Logger)
		message    string
		args       any
		wantEvents []Event
	}{
		{
			name: "Trace level",
			logFunc: func(ctx context.Context, l Logger) {
				l.Trace().WithCtx(ctx).Emitf("param matching: %v and %v", "param1", "param2")
			},
			message: "param matching: %v and %v",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelTrace,
							Severity:   LogSeverityTrace,
							Body:       "param matching: param1 and param2",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Debug level",
			logFunc: func(ctx context.Context, l Logger) {
				l.Debug().WithCtx(ctx).Emitf("param matching: %v and %v", "param1", "param2")
			},
			message: "param matching: %v and %v",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelDebug,
							Severity:   LogSeverityDebug,
							Body:       "param matching: param1 and param2",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Info level",
			logFunc: func(ctx context.Context, l Logger) {
				l.Info().WithCtx(ctx).Emitf("param matching: %v and %v", "param1", "param2")
			},
			message: "param matching: %v and %v",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelInfo,
							Severity:   LogSeverityInfo,
							Body:       "param matching: param1 and param2",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Warn level",
			logFunc: func(ctx context.Context, l Logger) {
				l.Warn().WithCtx(ctx).Emitf("param matching: %v and %v", "param1", "param2")
			},
			message: "param matching: %v and %v",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelWarn,
							Severity:   LogSeverityWarning,
							Body:       "param matching: param1 and param2",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Error level",
			logFunc: func(ctx context.Context, l Logger) {
				l.Error().WithCtx(ctx).Emitf("param matching: %v and %v", "param1", "param2")
			},
			message: "param matching: %v and %v",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelError,
							Severity:   LogSeverityError,
							Body:       "param matching: param1 and param2",
							Attributes: attrs,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := setupMockTransport()
			l := NewLogger(ctx)
			l.SetAttributes(
				attribute.Int("key.int", 42),
				attribute.Float64("key.float", 42.2),
				attribute.Bool("key.bool", true),
				attribute.String("key.string", "str"),
			)
			// invalid attribute should be dropped
			l.SetAttributes(attribute.Builder{Key: "key.invalid", Value: attribute.Value{}})
			tt.logFunc(ctx, l)
			Flush(testutils.FlushTimeout())

			opts := cmp.Options{
				cmpopts.IgnoreFields(Log{}, "Timestamp"),
			}

			gotEvents := mockTransport.Events()
			if len(gotEvents) != len(tt.wantEvents) {
				t.Fatalf("expected %d events, got %d", len(tt.wantEvents), len(gotEvents))
			}
			for i, event := range gotEvents {
				assertEqual(t, event.Type, logEvent.Type)
				if diff := cmp.Diff(tt.wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
				// pop used mock event
				mockTransport.events = nil
			}
		})
	}
}

func Test_sentryLogger_MethodsWithoutFormat(t *testing.T) {
	attrs := map[string]Attribute{
		"sentry.release":        {Value: "v1.2.3", Type: "string"},
		"sentry.environment":    {Value: "testing", Type: "string"},
		"sentry.server.address": {Value: "test-server", Type: "string"},
		"sentry.sdk.name":       {Value: "sentry.go", Type: "string"},
		"sentry.sdk.version":    {Value: "0.10.0", Type: "string"},
	}

	tests := []struct {
		name       string
		logFunc    func(ctx context.Context, l Logger, msg any)
		args       any
		wantEvents []Event
	}{
		{
			name: "Trace level",
			logFunc: func(ctx context.Context, l Logger, msg any) {
				l.Trace().WithCtx(ctx).Emit(msg)
			},
			args: "trace",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelTrace,
							Severity:   LogSeverityTrace,
							Body:       "trace",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Debug level",
			logFunc: func(ctx context.Context, l Logger, msg any) {
				l.Debug().WithCtx(ctx).Emit(msg)
			},
			args: "debug",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelDebug,
							Severity:   LogSeverityDebug,
							Body:       "debug",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Info level",
			logFunc: func(ctx context.Context, l Logger, msg any) {
				l.Info().WithCtx(ctx).Emit(msg)
			},
			args: "info",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelInfo,
							Severity:   LogSeverityInfo,
							Body:       "info",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Warn level",
			logFunc: func(ctx context.Context, l Logger, msg any) {
				l.Warn().WithCtx(ctx).Emit(msg)
			},
			args: "warn",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelWarn,
							Severity:   LogSeverityWarning,
							Body:       "warn",
							Attributes: attrs,
						},
					},
				},
			},
		},
		{
			name: "Error level",
			logFunc: func(ctx context.Context, l Logger, msg any) {
				l.Error().WithCtx(ctx).Emit(msg)
			},
			args: "error",
			wantEvents: []Event{
				{
					Logs: []Log{
						{
							TraceID:    TraceIDFromHex(LogTraceID),
							Level:      LogLevelError,
							Severity:   LogSeverityError,
							Body:       "error",
							Attributes: attrs,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := setupMockTransport()
			l := NewLogger(ctx)
			tt.logFunc(ctx, l, tt.args)
			Flush(testutils.FlushTimeout())

			opts := cmp.Options{
				cmpopts.IgnoreFields(Log{}, "Timestamp"),
			}

			gotEvents := mockTransport.Events()
			if len(gotEvents) != len(tt.wantEvents) {
				t.Fatalf("expected %d events, got %d", len(tt.wantEvents), len(gotEvents))
			}
			for i, event := range gotEvents {
				assertEqual(t, event.Type, logEvent.Type)
				if diff := cmp.Diff(tt.wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
				// pop used mock event
				mockTransport.events = nil
			}
		})
	}
}

func Test_sentryLogger_Panic(t *testing.T) {
	t.Run("logger.Panic", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic, but code did not panic")
			} else {
				t.Logf("recovered panic: %v", r)
			}
		}()
		ctx, _ := setupMockTransport()
		l := NewLogger(ctx)
		l.Panic().Emit("panic message") // This should panic
	})

	t.Run("logger.Panicf", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic, but code did not panic")
			} else {
				t.Logf("recovered panic: %v", r)
			}
		}()
		ctx, _ := setupMockTransport()
		l := NewLogger(ctx)
		l.Panic().Emitf("panic message") // This should panic
	})
}

func Test_sentryLogger_Write(t *testing.T) {
	msg := []byte("message from writer\n")
	attrs := map[string]Attribute{
		"sentry.release":        {Value: "v1.2.3", Type: "string"},
		"sentry.environment":    {Value: "testing", Type: "string"},
		"sentry.server.address": {Value: "test-server", Type: "string"},
		"sentry.sdk.name":       {Value: "sentry.go", Type: "string"},
		"sentry.sdk.version":    {Value: "0.10.0", Type: "string"},
	}
	wantLogs := []Log{
		{
			TraceID:    TraceIDFromHex(LogTraceID),
			Level:      LogLevelInfo,
			Severity:   LogSeverityInfo,
			Body:       "message from writer",
			Attributes: attrs,
		},
	}

	ctx, mockTransport := setupMockTransport()
	l := NewLogger(ctx)
	n, err := l.Write(msg)

	if err != nil {
		t.Errorf("Write returned unexpected error: %v", err)
	}
	if n != len(msg) {
		t.Errorf("Write returned wrong byte count: got %d, want %d", n, len(msg))
	}
	Flush(testutils.FlushTimeout())

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Type, logEvent.Type)

	opts := cmp.Options{
		cmpopts.IgnoreFields(Log{}, "Timestamp"),
	}
	if diff := cmp.Diff(wantLogs, event.Logs, opts); diff != "" {
		t.Errorf("Logs mismatch (-want +got):\n%s", diff)
	}
}

func Test_sentryLogger_FlushAttributesAfterSend(t *testing.T) {
	msg := []byte("something")
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(ctx)
	l.SetAttributes(attribute.Int("int", 42))
	l.Info().WithCtx(ctx).Emit(msg)

	l.SetAttributes(attribute.String("string", "some str"))
	l.Warn().WithCtx(ctx).Emit(msg)
	Flush(testutils.FlushTimeout())

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Logs[0].Attributes["int"].Value, int64(42))
	if _, ok := event.Logs[1].Attributes["int"]; !ok {
		t.Fatalf("expected key to exist")
	}
	assertEqual(t, event.Logs[1].Attributes["string"].Value, "some str")
}

func TestSentryLogger_LogEntryAttributes(t *testing.T) {
	msg := []byte("something")
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(ctx)
	l.Info().WithCtx(ctx).
		String("key.string", "some str").
		Int("key.int", 42).
		Int64("key.int64", 17).
		Float64("key.float", 42.2).
		Bool("key.bool", true).
		Emit(msg)

	Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Logs[0].Attributes["key.int"].Value, int64(42))
	assertEqual(t, event.Logs[0].Attributes["key.int64"].Value, int64(17))
	assertEqual(t, event.Logs[0].Attributes["key.float"].Value, 42.2)
	assertEqual(t, event.Logs[0].Attributes["key.bool"].Value, true)
	assertEqual(t, event.Logs[0].Attributes["key.string"].Value, "some str")
}

func Test_batchLogger_Flush(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	l.Info().WithCtx(ctx).Emit("context done log")
	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func Test_batchLogger_FlushWithContext(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	l.Info().WithCtx(ctx).Emit("context done log")

	cancelCtx, cancel := context.WithTimeout(context.Background(), testutils.FlushTimeout())
	FlushWithContext(cancelCtx)
	defer cancel()

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func Test_batchLogger_FlushMultipleTimes(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(ctx)

	for i := 0; i < 5; i++ {
		l.Info().WithCtx(ctx).Emit("test")
	}

	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Logf("Got %d events instead of 1", len(events))
		for i, event := range events {
			t.Logf("Event %d: %d logs", i, len(event.Logs))
		}
		t.Fatalf("expected 1 event after first flush, got %d", len(events))
	}
	if len(events[0].Logs) != 5 {
		t.Fatalf("expected 5 logs in first batch, got %d", len(events[0].Logs))
	}

	mockTransport.events = nil

	for i := 0; i < 3; i++ {
		l.Info().WithCtx(ctx).Emit("test")
	}

	Flush(testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after second flush, got %d", len(events))
	}
	if len(events[0].Logs) != 3 {
		t.Fatalf("expected 3 logs in second batch, got %d", len(events[0].Logs))
	}

	mockTransport.events = nil

	Flush(testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after third flush with no logs, got %d", len(events))
	}
}

func Test_batchLogger_Shutdown(t *testing.T) {
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:                    testDsn,
		Transport:              mockTransport,
		EnableLogs:             true,
		DisableTelemetryBuffer: true,
	})
	hub := CurrentHub()
	hub.BindClient(mockClient)
	ctx := SetHubOnContext(context.Background(), hub)
	l := NewLogger(ctx)
	for i := 0; i < 3; i++ {
		l.Info().WithCtx(ctx).Emit("test")
	}

	hub = GetHubFromContext(ctx)
	hub.Client().batchLogger.Shutdown()

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event after shutdown, got %d", len(events))
	}
	if len(events[0].Logs) != 3 {
		t.Fatalf("expected 3 logs in shutdown batch, got %d", len(events[0].Logs))
	}

	mockTransport.events = nil

	// Test that shutdown can be called multiple times safely
	hub.Client().batchLogger.Shutdown()
	hub.Client().batchLogger.Shutdown()

	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after multiple shutdowns, got %d", len(events))
	}

	Flush(testutils.FlushTimeout())
	events = mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected 0 events after flush on shutdown logger, got %d", len(events))
	}
}

func Test_sentryLogger_BeforeSendLog(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableLogs:    true,
		EnableTracing: true,
		BeforeSendLog: func(_ *Log) *Log {
			return nil
		},
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	ctx = SetHubOnContext(ctx, hub)

	l := NewLogger(ctx)
	l.Info().WithCtx(ctx).Emit("context done log")
	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func Test_Logger_ExceedBatchSize(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	for i := 0; i < 100; i++ {
		l.Info().WithCtx(ctx).Emit("test")
	}

	// sleep to wait for events to propagate
	time.Sleep(20 * time.Millisecond)
	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected only one event with 100 logs, got %d", len(events))
	}
}

func Test_sentryLogger_TracePropagationWithTransaction(t *testing.T) {
	ctx, mockTransport := setupMockTransport()

	// Start a new transaction
	txn := StartTransaction(ctx, "test-transaction")
	defer txn.Finish()

	expectedTraceID := txn.TraceID
	expectedSpanID := txn.SpanID

	logger := NewLogger(txn.Context())
	logger.Info().WithCtx(txn.Context()).Emit("message with tracing")

	Flush(testutils.FlushTimeout())

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	logs := events[0].Logs
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	log := logs[0]

	if log.TraceID != expectedTraceID {
		t.Errorf("unexpected TraceID: got %s, want %s", log.TraceID.String(), expectedTraceID.String())
	}
	if val, ok := log.Attributes["sentry.trace.parent_span_id"]; !ok {
		t.Errorf("missing sentry.trace.parent_span_id attribute")
	} else if val.Value != expectedSpanID.String() {
		t.Errorf("unexpected SpanID: got %s, want %s", val.Value, expectedSpanID.String())
	}
}

func TestSentryLogger_DebugLogging(t *testing.T) {
	tests := []struct {
		name       string
		enableLogs bool
		message    string
	}{
		{
			name:       "Debug enabled",
			enableLogs: true,
			message:    "test message",
		},
		{
			name:       "Debug disabled",
			enableLogs: false,
			message:    "test message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			ctx := context.Background()
			mockClient, _ := NewClient(ClientOptions{
				Transport:  &MockTransport{},
				EnableLogs: tt.enableLogs,
				Debug:      true,
			})
			hub := CurrentHub()
			hub.BindClient(mockClient)

			// set the debug logger output after NewClient, so that it doesn't change.
			debuglog.SetOutput(&buf)
			defer debuglog.SetOutput(io.Discard)

			logger := NewLogger(ctx)
			logger.Info().WithCtx(ctx).Emit(tt.message)

			got := buf.String()
			if tt.enableLogs {
				assertEqual(t, strings.Contains(got, "test message"), true)
			} else {
				assertEqual(t, strings.Contains(got, "test message"), false)
			}
		})
	}
}

func Test_sentryLogger_UserAttributes(t *testing.T) {
	ctx := context.Background()
	mockTransport := &MockTransport{}
	mockClient, _ := NewClient(ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableLogs:    true,
		EnableTracing: true,
	})
	mockClient.sdkIdentifier = "sentry.go"
	mockClient.sdkVersion = "0.10.0"
	hub := CurrentHub()
	hub.BindClient(mockClient)
	hub.Scope().propagationContext.TraceID = TraceIDFromHex(LogTraceID)

	hub.Scope().SetUser(User{
		ID:    "user123",
		Name:  "Test User",
		Email: "test@example.com",
	})

	ctx = SetHubOnContext(ctx, hub)

	l := NewLogger(ctx)
	l.Info().WithCtx(ctx).Emit("test message with PII")
	Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	logs := events[0].Logs
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}

	log := logs[0]
	attrs := log.Attributes

	if val, ok := attrs["user.id"]; !ok {
		t.Error("missing user.id attribute")
	} else if val.Value != "user123" {
		t.Errorf("unexpected user.id: got %v, want %v", val.Value, "user123")
	}

	if val, ok := attrs["user.name"]; !ok {
		t.Error("missing user.name attribute")
	} else if val.Value != "Test User" {
		t.Errorf("unexpected user.name: got %v, want %v", val.Value, "Test User")
	}

	if val, ok := attrs["user.email"]; !ok {
		t.Error("missing user.email attribute")
	} else if val.Value != "test@example.com" {
		t.Errorf("unexpected user.email: got %v, want %v", val.Value, "test@example.com")
	}
}

func TestLogEntryWithCtx_ShouldCopy(t *testing.T) {
	ctx, _ := setupMockTransport()
	l := NewLogger(ctx)

	// using WithCtx should return a new log entry with the new ctx
	newCtx := context.Background()
	lentry := l.Info().String("key", "value").(*logEntry)
	newlentry := lentry.WithCtx(newCtx).(*logEntry)
	lentry.String("key2", "value")

	assert.Equal(t, lentry.ctx, ctx)
	assert.Equal(t, newlentry.ctx, newCtx)
	assert.Contains(t, lentry.attributes, "key")
	assert.Contains(t, lentry.attributes, "key2")
	assert.Contains(t, newlentry.attributes, "key")
	assert.NotContains(t, newlentry.attributes, "key2")
}
