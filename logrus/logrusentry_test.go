package sentrylogrus

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	pkgerr "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func setupClientTest() (*sentry.Client, *sentry.MockTransport) {
	mockTransport := &sentry.MockTransport{}
	mockClient, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:        "http://whatever@example.com/1337",
		Transport:  mockTransport,
		EnableLogs: true,
	})
	hub := sentry.CurrentHub()
	hub.BindClient(mockClient)

	return mockClient, mockTransport
}

func TestNew(t *testing.T) {
	t.Parallel()
	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()
		_, err := New(nil, sentry.ClientOptions{Dsn: "%xxx"})
		if err == nil || !strings.Contains(err.Error(), "invalid URL escape") {
			t.Errorf("Unexpected error: %s", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		h, err := New(nil, sentry.ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}

		if !h.Flush(testutils.FlushTimeout()) {
			t.Error("flush failed")
		}
	})
}

func TestNewFromClient(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook := NewFromClient(levels, client)
	assert.NotNil(t, hook)
	assert.Equal(t, levels, hook.Levels())
}

func TestNewEventHook(t *testing.T) {
	t.Parallel()
	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()
		_, err := NewEventHook(nil, sentry.ClientOptions{Dsn: "%xxx"})
		if err == nil || !strings.Contains(err.Error(), "invalid URL escape") {
			t.Errorf("Unexpected error: %s", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		h, err := NewEventHook(nil, sentry.ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}

		if !h.Flush(testutils.FlushTimeout()) {
			t.Error("flush failed")
		}
	})
}

func TestNewEventHookFromClient(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook := NewEventHookFromClient(levels, client)
	assert.NotNil(t, hook)
	assert.Equal(t, levels, hook.Levels())
}

func TestEventHook_SetHubProvider(t *testing.T) {
	t.Parallel()

	h, err := NewEventHook(nil, sentry.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Custom HubProvider to ensure separate hubs for each test
	h.SetHubProvider(func() *sentry.Hub {
		client, _ := sentry.NewClient(sentry.ClientOptions{})
		return sentry.NewHub(client, sentry.NewScope())
	})

	entry := &logrus.Entry{Level: logrus.ErrorLevel}
	if err := h.Fire(entry); err != nil {
		t.Fatal(err)
	}

	if !h.Flush(testutils.FlushTimeout()) {
		t.Error("flush failed")
	}
}

func TestEventHook_Fire(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewEventHookFromClient([]logrus.Level{logrus.ErrorLevel}, client)

	t.Parallel()
	t.Run("successful capture", func(t *testing.T) {
		entry := &logrus.Entry{
			Level:   logrus.ErrorLevel,
			Message: "test error message",
			Data:    logrus.Fields{},
		}

		err := hook.Fire(entry)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(transport.Events()))
		assert.Equal(t, "test error message", transport.Events()[0].Message)
		assert.Equal(t, sentry.LevelError, transport.Events()[0].Level)
	})

	t.Run("with fallback", func(t *testing.T) {
		failClient, _ := sentry.NewClient(sentry.ClientOptions{
			Dsn: "http://whatever@example.com/1337",
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				return nil // Simulate failure by returning nil
			},
		})

		var fallbackCalled bool
		failHook := NewEventHookFromClient([]logrus.Level{logrus.ErrorLevel}, failClient)
		failHook.SetFallback(func(entry *logrus.Entry) error {
			fallbackCalled = true
			return nil
		})

		entry := &logrus.Entry{
			Level:   logrus.ErrorLevel,
			Message: "test error message",
			Data:    logrus.Fields{},
		}

		err := failHook.Fire(entry)
		assert.NoError(t, err)
		assert.True(t, fallbackCalled)
	})

	t.Run("capture fails no fallback", func(t *testing.T) {
		failClient, _ := sentry.NewClient(sentry.ClientOptions{
			Dsn: "http://whatever@example.com/1337",
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				return nil
			},
			Transport: &sentry.MockTransport{},
		})
		hookNoFallback := NewEventHookFromClient([]logrus.Level{logrus.ErrorLevel}, failClient)
		hookNoFallback.(*eventHook).fallback = nil

		entry := &logrus.Entry{Level: logrus.ErrorLevel, Message: "capture fail"}
		err := hookNoFallback.Fire(entry)
		assert.Error(t, err)
		assert.Equal(t, "failed to send to sentry", err.Error())
	})
}

func TestEventHookSetKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewEventHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	eventHook := hook.(*eventHook)

	// Test setting a key
	hook.SetKey("oldKey", "newKey")
	assert.Equal(t, "newKey", eventHook.keys["oldKey"])

	// Test deleting a key
	hook.SetKey("oldKey", "")
	_, exists := eventHook.keys["oldKey"]
	assert.False(t, exists)

	// Test empty oldKey does nothing
	eventHook.keys["test"] = "value"
	hook.SetKey("", "newKey")
	assert.Equal(t, "value", eventHook.keys["test"])
}

func TestEventHookKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewEventHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	eventHook := hook.(*eventHook)
	eventHook.keys["mappedKey"] = "newKey"
	assert.Equal(t, "newKey", eventHook.key("mappedKey"))
	assert.Equal(t, "unmappedKey", eventHook.key("unmappedKey"))
}

func TestEventHookLevels(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook := NewEventHookFromClient(levels, client)

	assert.Equal(t, levels, hook.Levels())
}

func TestEventHook_entryToEvent(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		entry *logrus.Entry
		want  *sentry.Event
	}{
		"empty entry": {
			entry: &logrus.Entry{},
			want: &sentry.Event{
				Level:  "fatal",
				Extra:  map[string]any{},
				Logger: name,
			},
		},
		"data fields": {
			entry: &logrus.Entry{
				Data: map[string]any{
					"foo": 123.4,
					"bar": "oink",
				},
			},
			want: &sentry.Event{
				Level:  sentry.LevelFatal,
				Extra:  map[string]any{"bar": "oink", "foo": 123.4},
				Logger: name,
			},
		},
		"info level": {
			entry: &logrus.Entry{
				Level: logrus.InfoLevel,
			},
			want: &sentry.Event{
				Level:  sentry.LevelInfo,
				Extra:  map[string]any{},
				Logger: name,
			},
		},
		"message": {
			entry: &logrus.Entry{
				Message: "the only thing we have to fear is fear itself",
			},
			want: &sentry.Event{
				Level:   sentry.LevelFatal,
				Extra:   map[string]any{},
				Message: "the only thing we have to fear is fear itself",
				Logger:  name,
			},
		},
		"timestamp": {
			entry: &logrus.Entry{
				Time: time.Unix(1, 2).UTC(),
			},
			want: &sentry.Event{
				Level:     sentry.LevelFatal,
				Extra:     map[string]any{},
				Timestamp: time.Unix(1, 2).UTC(),
				Logger:    name,
			},
		},
		"http request": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldRequest: httptest.NewRequest("GET", "/", nil),
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				Request: &sentry.Request{
					URL:     "http://example.com/",
					Method:  http.MethodGet,
					Headers: map[string]string{"Host": "example.com"},
				},
				Logger: name,
			},
		},
		"sentry request": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldRequest: sentry.Request{
						URL:    "http://example.com/",
						Method: http.MethodGet,
					},
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				Request: &sentry.Request{
					URL:    "http://example.com/",
					Method: http.MethodGet,
				},
				Logger: name,
			},
		},
		"sentry pointer to request": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldRequest: &sentry.Request{
						URL:    "http://example.com/",
						Method: http.MethodGet,
					},
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				Request: &sentry.Request{
					URL:    "http://example.com/",
					Method: http.MethodGet,
				},
				Logger: name,
			},
		},
		"error": {
			entry: &logrus.Entry{
				Data: map[string]any{
					logrus.ErrorKey: errors.New("things failed"),
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				Exception: []sentry.Exception{
					{Type: "*errors.errorString", Value: "things failed", Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{}}},
				},
				Logger: name,
			},
		},
		"non-error": {
			entry: &logrus.Entry{
				Data: map[string]any{
					logrus.ErrorKey: "this isn't really an error",
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{
					"error": "this isn't really an error",
				},
				Logger: name,
			},
		},
		"error with stack trace": {
			entry: &logrus.Entry{
				Data: map[string]any{
					logrus.ErrorKey: pkgerr.WithStack(errors.New("failure")),
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				Exception: []sentry.Exception{
					{
						Type:  "*errors.errorString",
						Value: "failure",
						Mechanism: &sentry.Mechanism{
							ExceptionID:      0,
							IsExceptionGroup: true,
							Type:             "generic",
						},
					},
					{
						Type:  "*errors.withStack",
						Value: "failure",
						Stacktrace: &sentry.Stacktrace{
							Frames: []sentry.Frame{},
						},
						Mechanism: &sentry.Mechanism{
							ExceptionID:      1,
							IsExceptionGroup: true,
							ParentID:         sentry.Pointer(0),
							Type:             "generic",
						},
					},
				},
				Logger: name,
			},
		},
		"user": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldUser: sentry.User{
						ID: "bob",
					},
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				User: sentry.User{
					ID: "bob",
				},
				Logger: name,
			},
		},
		"user pointer": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldUser: &sentry.User{
						ID: "alice",
					},
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{},
				User: sentry.User{
					ID: "alice",
				},
				Logger: name,
			},
		},
		"non-user": {
			entry: &logrus.Entry{
				Data: map[string]any{
					FieldUser: "just say no to drugs",
				},
			},
			want: &sentry.Event{
				Level: sentry.LevelFatal,
				Extra: map[string]any{
					"user": "just say no to drugs",
				},
				Logger: name,
			},
		},
		"transaction": {
			entry: &logrus.Entry{
				Level:   logrus.ErrorLevel,
				Message: "transaction error",
				Data:    logrus.Fields{FieldTransaction: "payment_process"},
			},
			want: &sentry.Event{
				Level:       sentry.LevelError,
				Message:     "transaction error",
				Extra:       map[string]any{},
				Transaction: "payment_process",
				Logger:      name,
			},
		},
		"fingerprint": {
			entry: &logrus.Entry{
				Level:   logrus.ErrorLevel,
				Message: "fingerprinted error",
				Data:    logrus.Fields{FieldFingerprint: []string{"{{ default }}", "custom-fingerprint"}},
			},
			want: &sentry.Event{
				Level:       sentry.LevelError,
				Message:     "fingerprinted error",
				Extra:       map[string]any{},
				Fingerprint: []string{"{{ default }}", "custom-fingerprint"},
				Logger:      name,
			},
		},
	}

	h, err := NewEventHook(nil, sentry.ClientOptions{
		AttachStacktrace: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	hook := h.(*eventHook)

	// Custom HubProvider for test environment
	h.SetHubProvider(func() *sentry.Hub {
		client, _ := sentry.NewClient(sentry.ClientOptions{})
		return sentry.NewHub(client, sentry.NewScope())
	})

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := hook.entryToEvent(tt.entry)
			opts := cmp.Options{
				cmpopts.IgnoreFields(sentry.Event{}, "sdkMetaData", "Contexts", "EventID", "Platform", "Release", "ServerName", "Modules", "Sdk", "Timestamp"),
				cmpopts.IgnoreFields(sentry.Stacktrace{}, "Frames"),
			}
			if d := cmp.Diff(tt.want, got, opts); d != "" {
				t.Errorf("entryToEvent mismatch (-want +got):\n%s", d)
			}
		})
	}
}

func TestEventHook_AddTags(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewEventHookFromClient(nil, client).(*eventHook)

	tags := map[string]string{"tag1": "value1", "tag2": "value2"}
	hook.AddTags(tags)
	hook.Fire(&logrus.Entry{})
	hook.Flush(testutils.FlushTimeout())
	got := transport.Events()
	assert.Equal(t, 1, len(got), "unexpected number of events")
	assert.Equal(t, tags, got[0].Tags)
}

func TestNewLogHook(t *testing.T) {
	// Test with valid options
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook, err := NewLogHook(levels, sentry.ClientOptions{
		Dsn:         "http://whatever@example.com/1337",
		Environment: "test",
		EnableLogs:  true,
	})

	assert.NoError(t, err)
	assert.NotNil(t, hook)
	assert.Equal(t, levels, hook.Levels())

	if !hook.Flush(testutils.FlushTimeout()) {
		t.Error("flush failed")
	}
	// Test with invalid options
	_, err = NewLogHook(levels, sentry.ClientOptions{
		Dsn: "invalid::dsn",
	})
	assert.Error(t, err)
}

func TestNewLogHookFromClient(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook := NewLogHookFromClient(levels, client)

	assert.NotNil(t, hook)
	assert.Equal(t, levels, hook.Levels())
}

func TestLogHookSetHubProvider(t *testing.T) {
	client, _ := setupClientTest()
	customHub := sentry.NewHub(client, sentry.NewScope())

	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)

	// Set a custom hub provider
	provider := func() *sentry.Hub { return customHub }
	hook.SetHubProvider(provider)

	// Verify the hub provider was set by checking it returns our custom hub
	logHook := hook.(*logHook)
	assert.Equal(t, customHub, logHook.hubProvider())
}

func TestLogHookSetFallback(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)

	var called bool
	fallback := FallbackFunc(func(entry *logrus.Entry) error {
		called = true
		return nil
	})

	hook.SetFallback(fallback)

	// Test fallback is set and can be called
	logHook := hook.(*logHook)
	entry := &logrus.Entry{Message: "test"}
	err := logHook.fallback(entry)

	assert.NoError(t, err)
	assert.True(t, called)
}

func TestLogHookSetKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	logHook := hook.(*logHook)

	// Test setting a key
	hook.SetKey("oldKey", "newKey")
	assert.Equal(t, "newKey", logHook.keys["oldKey"])

	// Test deleting a key
	hook.SetKey("oldKey", "")
	_, exists := logHook.keys["oldKey"]
	assert.False(t, exists)

	// Test empty oldKey does nothing
	logHook.keys["test"] = "value"
	hook.SetKey("", "newKey")
	assert.Equal(t, "value", logHook.keys["test"])
}

func TestLogHookKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	logHook := hook.(*logHook)

	// Set up a key mapping
	logHook.keys["mappedKey"] = "newKey"

	// Test mapped key returns the mapping
	assert.Equal(t, "newKey", logHook.key("mappedKey"))

	// Test unmapped key returns the original
	assert.Equal(t, "unmappedKey", logHook.key("unmappedKey"))
}

func TestLogHookLevels(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}
	hook := NewLogHookFromClient(levels, client)

	assert.Equal(t, levels, hook.Levels())
}

func TestLogHookFire(t *testing.T) {
	client, _ := setupClientTest()
	levels := []logrus.Level{
		logrus.TraceLevel, logrus.DebugLevel, logrus.InfoLevel,
		logrus.WarnLevel, logrus.ErrorLevel, logrus.FatalLevel,
	}
	hook := NewLogHookFromClient(levels, client)
	logHook := hook.(*logHook)

	tests := []struct {
		name  string
		level logrus.Level
	}{
		{"trace level", logrus.TraceLevel},
		{"debug level", logrus.DebugLevel},
		{"info level", logrus.InfoLevel},
		{"warn level", logrus.WarnLevel},
		{"error level", logrus.ErrorLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &logrus.Entry{
				Level:   tt.level,
				Data:    logrus.Fields{"key": "value"},
				Message: "test message",
				Context: context.Background(),
			}

			// Since we're using a real logger, which is hard to verify,
			// we're just checking that Fire doesn't error
			err := logHook.Fire(entry)
			assert.NoError(t, err)
		})
	}
}

func TestLogHook_AddTags(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client).(*logHook)

	tags := map[string]string{"tag1": "value1", "tag2": "value2"}
	hook.AddTags(tags)
	hook.Fire(&logrus.Entry{
		Context: context.Background(),
		Level:   logrus.InfoLevel,
		Message: "Something",
	})
	hook.Flush(testutils.FlushTimeout())
	got := transport.Events()
	assert.Equal(t, 1, len(got), "unexpected number of events")
	assert.Equal(t, tags["tag1"], got[0].Logs[0].Attributes["tag1"].Value)
	assert.Equal(t, tags["tag2"], got[0].Logs[0].Attributes["tag2"].Value)
}

func TestLogHookFireWithDifferentDataTypes(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	logHook := hook.(*logHook)

	type complexStruct struct {
		Name  string
		Value int
	}

	wantLog := sentry.Log{
		Level: sentry.LogLevelInfo,
		Body:  "test message",
		Attributes: map[string]sentry.Attribute{
			"int8":                {Value: int64(8), Type: "integer"},
			"int16":               {Value: int64(16), Type: "integer"},
			"int32":               {Value: int64(32), Type: "integer"},
			"int64":               {Value: int64(64), Type: "integer"},
			"int":                 {Value: int64(42), Type: "integer"},
			"uint8":               {Value: int64(8), Type: "integer"},
			"uint16":              {Value: int64(16), Type: "integer"},
			"uint32":              {Value: int64(32), Type: "integer"},
			"uint64":              {Value: int64(64), Type: "integer"},
			"uint":                {Value: int64(42), Type: "integer"},
			"string":              {Value: "test string", Type: "string"},
			"float32":             {Value: float64(float32(3.14)), Type: "double"},
			"float64":             {Value: 6.28, Type: "double"},
			"float64-overflow":    {Value: strconv.FormatUint(math.MaxUint64, 10), Type: "string"},
			"bool":                {Value: true, Type: "boolean"},
			"string_slice":        {Value: "[one two three]", Type: "string"},
			"string_map":          {Value: "map[a:1 b:2 c:3]", Type: "string"},
			"complex":             {Value: "{test 42}", Type: "string"},
			"sentry.origin":       {Value: "auto.logger.logrus", Type: "string"},
			"error.message":       {Value: "test error", Type: "string"},
			"error.type":          {Value: "*errors.errorString", Type: "string"},
			"http.request.method": {Value: "GET", Type: "string"},
			"url.full":            {Value: "https://example.com/test", Type: "string"},
			"user.email":          {Value: "test@example.com", Type: "string"},
			"user.id":             {Value: "test-user", Type: "string"},
			"user.name":           {Value: "tester", Type: "string"},
		},
	}

	entry := &logrus.Entry{
		Level: logrus.InfoLevel,
		Data: logrus.Fields{
			"int8":             int8(8),
			"int16":            int16(16),
			"int32":            int32(32),
			"int64":            int64(64),
			"int":              42,
			"uint8":            uint8(8),
			"uint16":           uint16(16),
			"uint32":           uint32(32),
			"uint64":           uint64(64),
			"uint":             uint(42),
			"string":           "test string",
			"float32":          float32(3.14),
			"float64":          float64(6.28),
			"float64-overflow": uint64(math.MaxUint64),
			"bool":             true,
			"error":            errors.New("test error"),
			"string_slice":     []string{"one", "two", "three"},
			"string_map":       map[string]string{"a": "1", "b": "2", "c": "3"},
			"complex":          complexStruct{Name: "test", Value: 42},
		},
		Message: "test message",
		Context: context.Background(),
	}

	// Add fields for request, user and transaction
	req, _ := http.NewRequest("GET", "https://example.com/test", nil)
	user := sentry.User{ID: "test-user", Email: "test@example.com", Name: "tester"}
	entry.Data[logHook.key(FieldRequest)] = req
	entry.Data[logHook.key(FieldUser)] = user
	entry.Data[logHook.key(FieldFingerprint)] = []string{"test-fingerprint"}

	err := logHook.Fire(entry)
	assert.NoError(t, err)

	logHook.Flush(testutils.FlushTimeout())
	got := transport.Events()
	assert.Equal(t, 1, len(got), "unexpected number of events")
	if diff := cmp.Diff(wantLog.Attributes, got[0].Logs[0].Attributes,
		cmpopts.IgnoreMapEntries(func(k string, v sentry.Attribute) bool {
			return k == "sentry.sdk.name" || k == "sentry.release" || k == "sentry.sdk.version" || k == "sentry.server.address"
		}),
	); diff != "" {
		t.Errorf("Attributes mismatch (-want +got):\n%s", diff)
	}
	assert.Equal(t, wantLog.Body, got[0].Logs[0].Body)
	assert.Equal(t, wantLog.Level, got[0].Logs[0].Level)
}

func TestLogHookFire_EventAndLogTypes(t *testing.T) {
	client, transport := setupClientTest()
	logger := logrus.New()

	logHook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	eventHook := NewEventHookFromClient([]logrus.Level{logrus.ErrorLevel}, client)
	logger.AddHook(logHook)
	logger.AddHook(eventHook)

	logger.Info("log")
	logger.Warn("should be skipped")
	logger.Error("event")

	logHook.Flush(testutils.FlushTimeout())
	eventHook.Flush(testutils.FlushTimeout())

	got := transport.Events()
	assert.Equal(t, 2, len(got), "unexpected number of events")
	for _, event := range got {
		if event.Type == "log" {
			assert.Equal(t, "log", event.Logs[0].Body)
			assert.Equal(t, sentry.LogLevelInfo, event.Logs[0].Level)
		} else {
			assert.Equal(t, "event", event.Message)
			assert.Equal(t, sentry.LevelError, event.Level)
		}
	}
}
