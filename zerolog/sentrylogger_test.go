package sentryzerolog

import (
	"context"
	"encoding/hex"
	"errors"
	"maps"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/rs/zerolog"
)

const (
	LogTraceID = "d49d9bf66f13450b81f65bc51cf49c03"
	testDsn    = "http://whatever@example.com/1337"
)

var baseAttributes = map[string]sentry.Attribute{
	"sentry.environment":    {Value: string("testing"), Type: "string"},
	"sentry.origin":         {Value: string("auto.logger.log"), Type: "string"},
	"sentry.release":        {Value: string("v1.2.3"), Type: "string"},
	"sentry.sdk.name":       {Value: string("sentry.go"), Type: "string"},
	"sentry.sdk.version":    {Value: string("0.33.0"), Type: "string"},
	"sentry.server.address": {Value: string("test-server"), Type: "string"},
}

func setupMockTransport() (context.Context, *sentry.MockTransport) {
	ctx := context.Background()
	mockTransport := &sentry.MockTransport{}
	mockClient, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:           testDsn,
		Transport:     mockTransport,
		Release:       "v1.2.3",
		Environment:   "testing",
		ServerName:    "test-server",
		EnableLogs:    true,
		EnableTracing: true,
	})
	hub := sentry.CurrentHub()
	hub.BindClient(mockClient)

	ctx = sentry.SetHubOnContext(ctx, hub)
	return ctx, mockTransport
}

func TestNewSentryLogger_Levels(t *testing.T) {
	attributes := map[string]sentry.Attribute{
		"foo": {Value: string("bar"), Type: "string"},
	}

	maps.Copy(attributes, baseAttributes)

	tests := []struct {
		name       string
		logFunc    func(ctx context.Context, l zerolog.Logger)
		wantEvents []sentry.Event
	}{
		{
			name: "Trace level",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Trace().Ctx(ctx).Str("foo", "bar").Msg("trace message")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelTrace,
							Severity:   sentry.LogSeverityTrace,
							Body:       "trace message",
							Attributes: attributes,
						},
					},
				},
			},
		},
		{
			name: "Debug level",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Debug().Ctx(ctx).Str("foo", "bar").Msg("debug message")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelDebug,
							Severity:   sentry.LogSeverityDebug,
							Body:       "debug message",
							Attributes: attributes,
						},
					},
				},
			},
		},
		{
			name: "Info level",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Info().Ctx(ctx).Str("foo", "bar").Msg("info message")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelInfo,
							Severity:   sentry.LogSeverityInfo,
							Body:       "info message",
							Attributes: attributes,
						},
					},
				},
			},
		},
		{
			name: "Warn level",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Warn().Ctx(ctx).Str("foo", "bar").Msg("warn message")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelWarn,
							Severity:   sentry.LogSeverityWarning,
							Body:       "warn message",
							Attributes: attributes,
						},
					},
				},
			},
		},
		{
			name: "Error level",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Error().Ctx(ctx).Str("foo", "bar").Msg("error message")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelError,
							Severity:   sentry.LogSeverityError,
							Body:       "error message",
							Attributes: attributes,
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := setupMockTransport()
			sentryZerolog := NewSentryLogger()
			l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
			tt.logFunc(ctx, l)
			if err := sentryZerolog.Close(); err != nil {
				t.Errorf("failed to close sentry zerolog: %v", err)
			}

			opts := cmp.Options{
				cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
			}

			gotEvents := mockTransport.Events()
			if len(gotEvents) != len(tt.wantEvents) {
				t.Fatalf("expected %d events, got %d", len(tt.wantEvents), len(gotEvents))
			}
			for i, event := range gotEvents {
				testutils.AssertEqual(t, event.Type, "log")
				if diff := cmp.Diff(tt.wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestNewSentryLogger_Fatal(t *testing.T) {
	t.Skip("TODO: make this test pass")

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but code did not panic")
		} else {
			t.Logf("recovered panic: %v", r)
		}
	}()

	ctx, _ := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Fatal().Ctx(ctx).Msg("fatal message")
}

func TestNewSentryLogger_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but code did not panic")
		} else {
			t.Logf("recovered panic: %v", r)
		}
	}()

	ctx, _ := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Panic().Ctx(ctx).Msg("panic message")
}

func TestNewSentryLogger_Attributes(t *testing.T) {
	tests := []struct {
		name           string
		logFunc        func(c *zerolog.Event) *zerolog.Event
		wantAttributes map[string]sentry.Attribute
	}{
		{
			name: "String attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Str("foo", "bar")
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: "bar", Type: "string"},
			},
		},
		{
			name: "Bool attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Bool("foo", true)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: true, Type: "boolean"},
			},
		},
		{
			name: "Float64 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Float64("foo", 10.0)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: 10.0, Type: "double"},
			},
		},
		{
			name: "Float32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Float32("foo", 10.0)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: 10.0, Type: "double"},
			},
		},
		{
			name: "Int attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Int8 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int8("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Int16 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int16("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Int32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int32("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Uint attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Uint8 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint8("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Uint16 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint16("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Uint32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint32("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Int64 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int64("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Uint64 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint64("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: float64(10), Type: "double"},
			},
		},
		{
			name: "Error attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Err(errors.New("test error"))
			},
			wantAttributes: map[string]sentry.Attribute{
				"error": {Value: "test error", Type: "string"},
			},
		},
		{
			name: "Hex attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				var buf [10]byte
				hex.Encode(buf[:], []byte("bar"))
				return c.Hex("foo", buf[:])
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: "36323631373200000000", Type: "string"},
			},
		},
		{
			name: "String array attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Strs("foo", []string{"bar", "baz"})
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo.0": {Value: "bar", Type: "string"},
				"foo.1": {Value: "baz", Type: "string"},
			},
		},
		{
			name: "Int array attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Ints("foo", []int{1, 2})
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo.0": {Value: float64(1), Type: "double"},
				"foo.1": {Value: float64(2), Type: "double"},
			},
		},
		{
			name: "Bool array attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Bools("foo", []bool{true, false})
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo.0": {Value: true, Type: "boolean"},
				"foo.1": {Value: false, Type: "boolean"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, mockTransport := setupMockTransport()
			sentryZerolog := NewSentryLogger()
			l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
			c := l.Info().Ctx(ctx)
			tt.logFunc(c)
			c.Msg("info message")
			if err := sentryZerolog.Close(); err != nil {
				t.Errorf("failed to close sentry zerolog: %v", err)
			}

			opts := cmp.Options{
				cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
			}

			// We only want to compare the attributes on this test.
			// Therefore it's much more makes sense to copy the attributes,
			// and set everything else to the same value.
			wantAttributes := tt.wantAttributes
			maps.Copy(wantAttributes, baseAttributes)

			wantEvents := []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:      sentry.LogLevelInfo,
							Severity:   sentry.LogSeverityInfo,
							Body:       "info message",
							Attributes: wantAttributes,
						},
					},
				},
			}

			gotEvents := mockTransport.Events()
			if len(gotEvents) != len(wantEvents) {
				t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
			}
			for i, event := range gotEvents {
				testutils.AssertEqual(t, event.Type, "log")
				if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestNewSentryLogger_EmptyMessage(t *testing.T) {
	ctx, mockTransport := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	// We don't want to have `Timestamp()` here.
	l := zerolog.New(sentryZerolog).With().Logger()
	// `Send` is the same as calling `Msg("")`
	l.Info().Ctx(ctx).Str("foo", "bar").Send()

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	wantAttributes := map[string]sentry.Attribute{"foo": {Value: "bar", Type: "string"}}
	maps.Copy(wantAttributes, baseAttributes)
	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelInfo,
					Severity:   sentry.LogSeverityInfo,
					Body:       "{\"level\":\"info\",\"foo\":\"bar\"}\n",
					Attributes: wantAttributes,
				},
			},
		},
	}
	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		testutils.AssertEqual(t, event.Type, "log")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestNewSentryLogger_WithoutContext(t *testing.T) {
	_, mockTransport := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Info().Str("foo", "bar").Msg("info message")
	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	wantAttributes := map[string]sentry.Attribute{"foo": {Value: "bar", Type: "string"}}
	maps.Copy(wantAttributes, baseAttributes)
	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelInfo,
					Severity:   sentry.LogSeverityInfo,
					Body:       "info message",
					Attributes: wantAttributes,
				},
			},
		},
	}
	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		testutils.AssertEqual(t, event.Type, "log")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_Write(t *testing.T) {
	_, mockTransport := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	n, err := sentryZerolog.Write([]byte(`{"message":"debug message"}`))
	if err != nil {
		t.Errorf("failed to write to sentry zerolog: %v", err)
	}
	if n != 27 {
		t.Errorf("expected to write 27 bytes, got %d", n)
	}

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelDebug,
					Severity:   sentry.LogSeverityDebug,
					Body:       "debug message",
					Attributes: baseAttributes,
				},
			},
		},
	}
	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		testutils.AssertEqual(t, event.Type, "log")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_ParseError(t *testing.T) {
	sentryZerolog := NewSentryLogger()
	n, err := sentryZerolog.Write([]byte(`this should trigger an error`))
	expectedError := "cannot decode event: invalid character 'h' in literal true (expecting 'r')"
	if err == nil {
		t.Errorf("expected error, got nil")
	} else if err.Error() != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, err.Error())
	}

	if n != 0 {
		t.Errorf("expected to write 0 bytes, got %d", n)
	}
}

func TestSentryLogger_UnparsableLevel(t *testing.T) {
	_, mockTransport := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	_, err := sentryZerolog.Write([]byte(`{"message":"debug message","level":"invalid"}`))
	if err != nil {
		t.Errorf("failed to write to sentry zerolog: %v", err)
	}

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelDebug,
					Severity:   sentry.LogSeverityDebug,
					Body:       "debug message",
					Attributes: baseAttributes,
				},
			},
		},
	}
	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		testutils.AssertEqual(t, event.Type, "log")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_DisabledLevel(t *testing.T) {
	_, mockTransport := setupMockTransport()
	sentryZerolog := NewSentryLogger()
	_, err := sentryZerolog.Write([]byte(`{"message":"debug message","level":"disabled"}`))
	if err != nil {
		t.Errorf("failed to write to sentry zerolog: %v", err)
	}

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	gotEvents := mockTransport.Events()
	if len(gotEvents) != 0 {
		t.Fatalf("expected %d events, got %d", 0, len(gotEvents))
	}
}
