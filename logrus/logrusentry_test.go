package sentrylogrus

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func setupClientTest() (*sentry.Client, *sentry.MockTransport) {
	mockTransport := &sentry.MockTransport{}
	mockClient, _ := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "http://whatever@example.com/1337",
		Transport: mockTransport,
	})
	hub := sentry.CurrentHub()
	hub.BindClient(mockClient)

	return mockClient, mockTransport
}

func TestNewLogHook(t *testing.T) {
	t.Parallel()

	levels := []logrus.Level{logrus.InfoLevel, logrus.ErrorLevel}

	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()

		_, err := NewLogHook(levels, sentry.ClientOptions{Dsn: "%xxx"})
		if err == nil || !strings.Contains(err.Error(), "invalid URL escape") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("DisableLogs", func(t *testing.T) {
		t.Parallel()

		_, err := NewLogHook(levels, sentry.ClientOptions{DisableLogs: true})
		assert.EqualError(t, err, "cannot create log hook, DisableLogs is set to true")
	})

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		hook, err := NewLogHook(levels, sentry.ClientOptions{})
		assert.NoError(t, err)
		assert.Equal(t, levels, hook.Levels())
		assert.True(t, hook.Flush(testutils.FlushTimeout()))
	})
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
	hook.SetHubProvider(func() *sentry.Hub { return customHub })

	assert.Equal(t, customHub, hook.(*logHook).hubProvider())
}

func TestLogHookSetFallback(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)

	var called bool
	fallback := FallbackFunc(func(_ *logrus.Entry) error {
		called = true
		return nil
	})

	hook.SetFallback(fallback)

	err := hook.(*logHook).fallback(&logrus.Entry{Message: "test"})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestLogHookSetKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	logHook := hook.(*logHook)

	hook.SetKey("oldKey", "newKey")
	assert.Equal(t, "newKey", logHook.keys["oldKey"])

	hook.SetKey("oldKey", "")
	_, exists := logHook.keys["oldKey"]
	assert.False(t, exists)

	logHook.keys["test"] = "value"
	hook.SetKey("", "newKey")
	assert.Equal(t, "value", logHook.keys["test"])
}

func TestLogHookKey(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client)
	logHook := hook.(*logHook)

	logHook.keys["mappedKey"] = "newKey"
	assert.Equal(t, "newKey", logHook.key("mappedKey"))
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
	hook := NewLogHookFromClient([]logrus.Level{
		logrus.TraceLevel,
		logrus.DebugLevel,
		logrus.InfoLevel,
		logrus.WarnLevel,
		logrus.ErrorLevel,
		logrus.FatalLevel,
	}, client).(*logHook)

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
			err := hook.Fire(&logrus.Entry{
				Level:   tt.level,
				Data:    logrus.Fields{"key": "value"},
				Message: "test message",
				Context: context.Background(),
			})
			assert.NoError(t, err)
		})
	}
}

func TestLogHookFireInvalidLevelUsesFallback(t *testing.T) {
	client, _ := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client).(*logHook)

	var called bool
	hook.SetFallback(func(_ *logrus.Entry) error {
		called = true
		return nil
	})

	err := hook.Fire(&logrus.Entry{
		Level:   logrus.Level(999),
		Message: "test",
	})
	assert.NoError(t, err)
	assert.True(t, called)
}

func TestLogHookAddTags(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client).(*logHook)

	tags := map[string]string{"tag1": "value1", "tag2": "value2"}
	hook.AddTags(tags)
	assert.NoError(t, hook.Fire(&logrus.Entry{
		Context: context.Background(),
		Level:   logrus.InfoLevel,
		Message: "something",
	}))

	hook.Flush(testutils.FlushTimeout())

	got := transport.Events()
	assert.Equal(t, 1, len(got))
	assert.Equal(t, tags["tag1"], got[0].Logs[0].Attributes["tag1"].String())
	assert.Equal(t, tags["tag2"], got[0].Logs[0].Attributes["tag2"].String())
}

func TestLogHookFireWithDifferentDataTypes(t *testing.T) {
	client, transport := setupClientTest()
	hook := NewLogHookFromClient([]logrus.Level{logrus.InfoLevel}, client).(*logHook)

	type complexStruct struct {
		Name  string
		Value int
	}

	wantAttrs := map[string]attribute.Value{
		"bool":          attribute.BoolValue(true),
		"complex":       attribute.StringValue("{test 42}"),
		"error":         attribute.StringValue("test error"),
		"float32":       attribute.Float64Value(float64(float32(3.14))),
		"float64":       attribute.Float64Value(6.28),
		"int":           attribute.Int64Value(42),
		"int16":         attribute.Int64Value(16),
		"int32":         attribute.Int64Value(32),
		"int64":         attribute.Int64Value(64),
		"int8":          attribute.Int64Value(8),
		"request":       attribute.StringValue("custom-request"),
		"sentry.origin": attribute.StringValue("auto.log.logrus"),
		"string":        attribute.StringValue("test string"),
		"string_map":    attribute.StringValue("map[a:1 b:2 c:3]"),
		"string_slice":  attribute.StringValue("[one two three]"),
		"transaction":   attribute.StringValue("payment"),
		"user":          attribute.StringValue("map[id:bob]"),
		"uint":          attribute.Uint64Value(42),
		"uint16":        attribute.Uint64Value(16),
		"uint32":        attribute.Uint64Value(32),
		"uint64":        attribute.Uint64Value(64),
		"uint8":         attribute.Uint64Value(8),
		"custom_key":    attribute.StringValue("[default custom]"),
	}

	hook.SetKey(FieldFingerprint, "custom_key")

	err := hook.Fire(&logrus.Entry{
		Level: logrus.InfoLevel,
		Data: logrus.Fields{
			"bool":           true,
			"complex":        complexStruct{Name: "test", Value: 42},
			"error":          errors.New("test error"),
			"float32":        float32(3.14),
			"float64":        6.28,
			"int":            42,
			"int8":           int8(8),
			"int16":          int16(16),
			"int32":          int32(32),
			"int64":          int64(64),
			FieldFingerprint: []string{"default", "custom"},
			FieldGoVersion:   "skip-me",
			FieldMaxProcs:    4,
			FieldRequest:     "custom-request",
			"string":         "test string",
			"string_map":     map[string]string{"a": "1", "b": "2", "c": "3"},
			"string_slice":   []string{"one", "two", "three"},
			FieldTransaction: "payment",
			FieldUser:        map[string]string{"id": "bob"},
			"uint":           uint(42),
			"uint8":          uint8(8),
			"uint16":         uint16(16),
			"uint32":         uint32(32),
			"uint64":         uint64(64),
		},
		Message: "test message",
		Context: context.Background(),
	})
	assert.NoError(t, err)

	hook.Flush(testutils.FlushTimeout())

	got := transport.Events()
	assert.Equal(t, 1, len(got))
	if diff := cmp.Diff(wantAttrs, got[0].Logs[0].Attributes,
		cmp.AllowUnexported(attribute.Value{}),
		cmpopts.IgnoreMapEntries(func(k string, _ attribute.Value) bool {
			return k == "sentry.sdk.name" || k == "sentry.release" || k == "sentry.sdk.version" || k == "sentry.server.address"
		}),
	); diff != "" {
		t.Errorf("Attributes mismatch (-want +got):\n%s", diff)
	}
	assert.Equal(t, "test message", got[0].Logs[0].Body)
	assert.Equal(t, sentry.LogLevelInfo, got[0].Logs[0].Level)
}
