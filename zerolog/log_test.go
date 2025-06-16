package sentryzerolog

import (
	"context"
	"encoding/hex"
	"errors"
	"maps"
	"math"
	"strconv"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

const (
	LogTraceID = "d49d9bf66f13450b81f65bc51cf49c03"
	testDsn    = "http://whatever@example.com/1337"
)

var baseAttributes = map[string]sentry.Attribute{
	"sentry.environment":    {Value: "testing", Type: "string"},
	"sentry.origin":         {Value: zerologOrigin, Type: "string"},
	"sentry.release":        {Value: "v1.2.3", Type: "string"},
	"sentry.sdk.name":       {Value: sdkIdentifier, Type: "string"},
	"sentry.sdk.version":    {Value: sentry.SDKVersion, Type: "string"},
	"sentry.server.address": {Value: "test-server", Type: "string"},
}

// defaultTestOptions contains the standard options used across most tests
var defaultTestOptions = Options{
	Levels: []zerolog.Level{
		zerolog.TraceLevel,
		zerolog.DebugLevel,
		zerolog.InfoLevel,
		zerolog.WarnLevel,
		zerolog.ErrorLevel,
		zerolog.FatalLevel,
		zerolog.PanicLevel,
	},
}

func setupMockTransport() (sentry.ClientOptions, *sentry.MockTransport) {
	mockTransport := &sentry.MockTransport{}
	clientOptions := sentry.ClientOptions{
		Dsn:         testDsn,
		Transport:   mockTransport,
		Release:     "v1.2.3",
		Environment: "testing",
		ServerName:  "test-server",
		EnableLogs:  true,
	}
	return clientOptions, mockTransport
}

func TestNewLogWriter_Levels(t *testing.T) {
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
			clientOptions, mockTransport := setupMockTransport()
			sentryZerolog, err := NewLogWriter(Config{
				ClientOptions: clientOptions,
				Options:       defaultTestOptions,
			})
			require.NoError(t, err)
			l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
			tt.logFunc(context.Background(), l)
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
				require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
				if diff := cmp.Diff(tt.wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestNewLogWriter_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but code did not panic")
		} else {
			t.Logf("recovered panic: %v", r)
		}
	}()

	clientOptions, _ := setupMockTransport()
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       defaultTestOptions,
	})
	require.NoError(t, err)
	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Panic().Ctx(context.Background()).Msg("panic message")
}

func TestNewLogWriter_Attributes(t *testing.T) {
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
				return c.Float64("foo", 10.2)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: 10.2, Type: "double"},
			},
		},
		{
			name: "Float32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Float32("foo", 10.2)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: 10.2, Type: "double"},
			},
		},
		{
			name: "Int attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Int8 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int8("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Int16 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int16("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Int32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int32("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Int64 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Int64("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint8 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint8("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint16 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint16("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint32 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint32("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint64 attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint64("foo", 10)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: int64(10), Type: "integer"},
			},
		},
		{
			name: "Uint64 attribute - overflow",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Uint64("foo", math.MaxUint64)
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: strconv.FormatUint(math.MaxUint64, 10), Type: "string"},
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
				"foo": {Value: "[bar baz]", Type: "string"},
			},
		},
		{
			name: "Int array attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Ints("foo", []int{1, 2})
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: "[1 2]", Type: "string"},
			},
		},
		{
			name: "Bool array attribute",
			logFunc: func(c *zerolog.Event) *zerolog.Event {
				return c.Bools("foo", []bool{true, false})
			},
			wantAttributes: map[string]sentry.Attribute{
				"foo": {Value: "[true false]", Type: "string"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientOptions, mockTransport := setupMockTransport()
			sentryZerolog, err := NewLogWriter(Config{
				ClientOptions: clientOptions,
				Options:       defaultTestOptions,
			})
			require.NoError(t, err)
			l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
			c := l.Info().Ctx(context.Background())
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
				require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
				if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestNewLogWriter_EmptyMessage(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       defaultTestOptions,
	})
	require.NoError(t, err)
	// We don't want to have `Timestamp()` here.
	l := zerolog.New(sentryZerolog).With().Logger()
	// `Send` is the same as calling `Msg("")`
	l.Info().Ctx(context.Background()).Str("foo", "bar").Send()

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
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestNewLogWriter_WithoutContext(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       defaultTestOptions,
	})
	require.NoError(t, err)
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
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_Write(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       defaultTestOptions,
	})
	require.NoError(t, err)
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
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_UnparsableLevel(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       defaultTestOptions,
	})
	require.NoError(t, err)
	_, err = sentryZerolog.Write([]byte(`{"message":"debug message","level":"invalid"}`))
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
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestSentryLogger_DisabledLevel(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	// Use custom options that exclude debug level to test disabled functionality
	disabledLevelOptions := Options{
		Levels: []zerolog.Level{
			zerolog.InfoLevel,
			zerolog.WarnLevel,
			zerolog.ErrorLevel,
			zerolog.FatalLevel,
			zerolog.PanicLevel,
		},
	}
	sentryZerolog, err := NewLogWriter(Config{
		ClientOptions: clientOptions,
		Options:       disabledLevelOptions,
	})
	require.NoError(t, err)
	_, err = sentryZerolog.Write([]byte(`{"message":"debug message","level":"debug"}`))
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

func TestNewLogWriterWithHub(t *testing.T) {
	tests := []struct {
		name       string
		logFunc    func(ctx context.Context, l zerolog.Logger)
		wantEvents []sentry.Event
	}{
		{
			name: "Info level with hub",
			logFunc: func(ctx context.Context, l zerolog.Logger) {
				l.Info().Ctx(ctx).Str("test", "hub").Msg("info message with hub")
			},
			wantEvents: []sentry.Event{
				{
					Logs: []sentry.Log{
						{
							Level:    sentry.LogLevelInfo,
							Severity: sentry.LogSeverityInfo,
							Body:     "info message with hub",
							Attributes: map[string]sentry.Attribute{
								"test":                  {Value: "hub", Type: "string"},
								"sentry.environment":    {Value: "testing", Type: "string"},
								"sentry.origin":         {Value: zerologOrigin, Type: "string"},
								"sentry.release":        {Value: "v1.2.3", Type: "string"},
								"sentry.sdk.name":       {Value: sdkIdentifier, Type: "string"},
								"sentry.sdk.version":    {Value: sentry.SDKVersion, Type: "string"},
								"sentry.server.address": {Value: "test-server", Type: "string"},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientOptions, mockTransport := setupMockTransport()
			client, err := sentry.NewClient(clientOptions)
			require.NoError(t, err)

			hub := sentry.NewHub(client, sentry.NewScope())
			sentryZerolog, err := NewLogWriterWithHub(hub, defaultTestOptions)
			require.NoError(t, err)

			l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
			tt.logFunc(context.Background(), l)
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
				require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
				if diff := cmp.Diff(tt.wantEvents[i].Logs, event.Logs, opts); diff != "" {
					t.Errorf("Log mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestNewLogWriterWithHub_NilHub(t *testing.T) {
	_, err := NewLogWriterWithHub(nil, defaultTestOptions)
	require.Error(t, err)
	require.Contains(t, err.Error(), "hub or client cannot be nil")
}

func TestNewLogWriter_UserAttributes(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	client, err := sentry.NewClient(clientOptions)
	require.NoError(t, err)

	hub := sentry.NewHub(client, sentry.NewScope())

	sentryZerolog, err := NewLogWriterWithHub(hub, defaultTestOptions)
	require.NoError(t, err)

	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Info().Ctx(context.Background()).
		Str("key", "value").
		Interface("user", sentry.User{ID: "test-user-id", Email: "test@sentry.io", Name: "Test User"}).
		Msg("test message")

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	wantUserAttributes := map[string]sentry.Attribute{
		"key":                   {Value: "value", Type: "string"},
		"user.id":               {Value: "test-user-id", Type: "string"},
		"user.name":             {Value: "Test User", Type: "string"},
		"user.email":            {Value: "test@sentry.io", Type: "string"},
		"sentry.environment":    {Value: "testing", Type: "string"},
		"sentry.origin":         {Value: zerologOrigin, Type: "string"},
		"sentry.release":        {Value: "v1.2.3", Type: "string"},
		"sentry.sdk.name":       {Value: sdkIdentifier, Type: "string"},
		"sentry.sdk.version":    {Value: sentry.SDKVersion, Type: "string"},
		"sentry.server.address": {Value: "test-server", Type: "string"},
	}

	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelInfo,
					Severity:   sentry.LogSeverityInfo,
					Body:       "test message",
					Attributes: wantUserAttributes,
				},
			},
		},
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}
	}
}

func TestNewLogWriter_UserAttributesPartial(t *testing.T) {
	clientOptions, mockTransport := setupMockTransport()
	client, err := sentry.NewClient(clientOptions)
	require.NoError(t, err)

	hub := sentry.NewHub(client, sentry.NewScope())

	sentryZerolog, err := NewLogWriterWithHub(hub, defaultTestOptions)
	require.NoError(t, err)

	l := zerolog.New(sentryZerolog).With().Timestamp().Logger()
	l.Info().Ctx(context.Background()).
		Str("key", "value").
		Interface("user", sentry.User{ID: "test-user-id", Name: "Test User"}).
		Msg("test message")

	if err := sentryZerolog.Close(); err != nil {
		t.Errorf("failed to close sentry zerolog: %v", err)
	}

	wantUserAttributes := map[string]sentry.Attribute{
		"key":       {Value: "value", Type: "string"},
		"user.id":   {Value: "test-user-id", Type: "string"},
		"user.name": {Value: "Test User", Type: "string"},
		// No user.email should be present
		"sentry.environment":    {Value: "testing", Type: "string"},
		"sentry.origin":         {Value: zerologOrigin, Type: "string"},
		"sentry.release":        {Value: "v1.2.3", Type: "string"},
		"sentry.sdk.name":       {Value: sdkIdentifier, Type: "string"},
		"sentry.sdk.version":    {Value: sentry.SDKVersion, Type: "string"},
		"sentry.server.address": {Value: "test-server", Type: "string"},
	}

	wantEvents := []sentry.Event{
		{
			Logs: []sentry.Log{
				{
					Level:      sentry.LogLevelInfo,
					Severity:   sentry.LogSeverityInfo,
					Body:       "test message",
					Attributes: wantUserAttributes,
				},
			},
		},
	}

	opts := cmp.Options{
		cmpopts.IgnoreFields(sentry.Log{}, "Timestamp", "TraceID"),
	}

	gotEvents := mockTransport.Events()
	if len(gotEvents) != len(wantEvents) {
		t.Fatalf("expected %d events, got %d", len(wantEvents), len(gotEvents))
	}
	for i, event := range gotEvents {
		require.NotEmpty(t, event.Logs, "event.Logs should not be empty")
		if diff := cmp.Diff(wantEvents[i].Logs, event.Logs, opts); diff != "" {
			t.Errorf("Log mismatch (-want +got):\n%s", diff)
		}

		// Verify user.email is not present
		for _, log := range event.Logs {
			if _, exists := log.Attributes["user.email"]; exists {
				t.Error("user.email should not be present when not set in scope")
			}
		}
	}
}
