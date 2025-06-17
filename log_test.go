package sentry

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
				l.Tracef(ctx, "param matching: %v and %v", "param1", "param2")
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
				l.Debugf(ctx, "param matching: %v and %v", "param1", "param2")
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
				l.Infof(ctx, "param matching: %v and %v", "param1", "param2")
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
				l.Warnf(ctx, "param matching: %v and %v", "param1", "param2")
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
				l.Errorf(ctx, "param matching: %v and %v", "param1", "param2")
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
			Flush(20 * time.Millisecond)

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
				l.Trace(ctx, msg)
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
				l.Debug(ctx, msg)
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
				l.Info(ctx, msg)
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
				l.Warn(ctx, msg)
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
				l.Error(ctx, msg)
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
			Flush(20 * time.Millisecond)

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
		l.Panic(context.Background(), "panic message") // This should panic
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
		l.Panicf(context.Background(), "panic message") // This should panic
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
	Flush(20 * time.Millisecond)

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
	l.Info(ctx, msg)

	l.SetAttributes(attribute.String("string", "some str"))
	l.Warn(ctx, msg)
	Flush(20 * time.Millisecond)

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 1 {
		t.Fatalf("expected 1 event, got %d", len(gotEvents))
	}
	event := gotEvents[0]
	assertEqual(t, event.Logs[0].Attributes["int"].Value, int64(42))
	if _, ok := event.Logs[1].Attributes["int"]; ok {
		t.Fatalf("expected key to not exist")
	}
	assertEqual(t, event.Logs[1].Attributes["string"].Value, "some str")
}

func Test_batchLogger_Flush(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	l.Info(ctx, "context done log")
	Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
}

func Test_batchLogger_FlushWithContext(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	l.Info(ctx, "context done log")

	cancelCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	FlushWithContext(cancelCtx)
	defer cancel()

	events := mockTransport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
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
	l.Info(ctx, "context done log")
	Flush(20 * time.Millisecond)

	events := mockTransport.Events()
	if len(events) != 0 {
		t.Fatalf("expected no events, got %d", len(events))
	}
}

func Test_Logger_ExceedBatchSize(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	l := NewLogger(context.Background())
	for i := 0; i < 100; i++ {
		l.Info(ctx, "test")
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
	logger.Info(txn.Context(), "message with tracing")

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
	var buf bytes.Buffer
	debugLogger := log.New(&buf, "", 0)
	originalLogger := DebugLogger
	DebugLogger = debugLogger
	defer func() {
		DebugLogger = originalLogger
	}()

	tests := []struct {
		name          string
		debugEnabled  bool
		message       string
		expectedDebug string
	}{
		{
			name:          "Debug enabled",
			debugEnabled:  true,
			message:       "test message",
			expectedDebug: "test message\n",
		},
		{
			name:          "Debug disabled",
			debugEnabled:  false,
			message:       "test message",
			expectedDebug: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			ctx := context.Background()
			mockClient, _ := NewClient(ClientOptions{
				Transport:  &MockTransport{},
				EnableLogs: true,
				Debug:      tt.debugEnabled,
			})
			hub := CurrentHub()
			hub.BindClient(mockClient)

			logger := NewLogger(ctx)
			logger.Info(ctx, tt.message)

			got := buf.String()
			if !tt.debugEnabled {
				assertEqual(t, len(got), 0)
			} else if strings.Contains(got, tt.expectedDebug) {
				t.Errorf("Debug output = %q, want %q", got, tt.expectedDebug)
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
	l.Info(ctx, "test message with PII")
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
