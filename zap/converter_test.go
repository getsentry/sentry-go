package sentryzap

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestZapFieldToLogEntry(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	entry := sentry.NewMockLogEntry()
	fields := []zapcore.Field{
		zap.Bool("enabled", true),
		zap.Bool("disabled", false),
		zap.Int64("count", 42),
		zap.Int32("count32", 32),
		zap.Int64("negative", -42),
		zap.Uint32("ucount", 32),
		zap.Float64("pi", 3.14159),
		zap.Float32("approx", 2.5),
		zap.String("message", "hello"),
		zap.String("empty", ""),
		zap.Duration("elapsed", 5*time.Second),
		zap.Time("timestamp", fixedTime),
		zap.Error(errors.New("something went wrong")),
		zap.Stringer("addr", &testStringer{value: "192.168.1.1"}),
		zap.ByteString("data", []byte("hello bytes")),
		zap.Binary("binary", []byte{0xde, 0xad, 0xbe, 0xef}),
		zap.Uintptr("ptr", uintptr(0x1234)),
	}

	for _, field := range fields {
		zapFieldToLogEntry(entry, field)
	}

	expected := map[string]any{
		"enabled":  true,
		"disabled": false,
		"count":    int64(42),
		"count32":  int64(32),
		"negative": int64(-42),
		"ucount":   int64(32),
		"pi":       3.14159,
		"approx":   2.5,
		"message":  "hello",
		"empty":    "",
		"elapsed":  "5s",
		"error":    "something went wrong",
		"addr":     "192.168.1.1",
		"data":     "hello bytes",
		"binary":   "3q2+7w==",
		"ptr":      "0x1234",
	}

	// Ignore timestamp key for cmp.Diff since time formatting can vary by locale
	if diff := cmp.Diff(expected, entry.Attributes, cmpopts.IgnoreMapEntries(func(k string, v any) bool {
		return k == "timestamp"
	}), cmpopts.EquateApprox(0, 0.0001)); diff != "" {
		t.Errorf("Attributes mismatch (-want +got):\n%s", diff)
	}

	timestamp, ok := entry.Attributes["timestamp"].(string)
	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		t.Fatalf("unexpected timestamp format: %v", timestamp)
	}
	assert.True(t, ok, "timestamp should be a string")
	assert.Contains(t, timestamp, "2024-01-15")
}

func TestZapFieldToLogEntry_EdgeCases(t *testing.T) {
	t.Run("nil error is skipped", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Error(nil))
		_, found := entry.Attributes["error"]
		assert.False(t, found)
	})

	t.Run("skip field", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Skip())
		assert.Empty(t, entry.Attributes)
	})

	t.Run("reflect type", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Reflect("custom", customStruct{Name: "test", Value: 42}))
		result, ok := entry.Attributes["custom"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "test")
		assert.Contains(t, result, "42")
	})

	t.Run("object marshaler", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Object("user", &testObjectMarshaler{name: "john", age: 30}))
		result, ok := entry.Attributes["user"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "john")
		assert.Contains(t, result, "30")
	})

	t.Run("array marshaler", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Array("items", &testArrayMarshaler{items: []string{"a", "b", "c"}}))
		result, ok := entry.Attributes["items"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "a")
		assert.Contains(t, result, "b")
		assert.Contains(t, result, "c")
	})

	t.Run("inline marshaler", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Inline(&testInlineMarshaler{name: "alice", score: 95}))
		result, ok := entry.Attributes[""].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "alice")
		assert.Contains(t, result, "95")
	})

	t.Run("complex64 type", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Complex64("c64", complex(float32(3.14), float32(2.71))))
		result, ok := entry.Attributes["c64"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "3.14")
		assert.Contains(t, result, "2.71")
	})

	t.Run("complex128 type", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Complex128("c128", complex(3.14159, 2.71828)))
		result, ok := entry.Attributes["c128"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "3.14159")
		assert.Contains(t, result, "2.71828")
	})

	t.Run("time full type", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		fixedTime := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
		// Create a TimeFullType field manually
		field := zapcore.Field{
			Key:       "full_time",
			Type:      zapcore.TimeFullType,
			Interface: fixedTime,
		}
		zapFieldToLogEntry(entry, field)
		result, ok := entry.Attributes["full_time"].(string)
		assert.True(t, ok)
		assert.Contains(t, result, "2024-01-15")
	})

	t.Run("namespace type", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()
		zapFieldToLogEntry(entry, zap.Namespace("http"))
		// Namespace fields should be skipped and not add any attributes
		assert.Empty(t, entry.Attributes)
	})

	t.Run("uint64 overflow", func(t *testing.T) {
		ctx, mockTransport := newMockTransport()
		core := NewSentryCore(ctx, Option{
			Level: []zapcore.Level{zapcore.InfoLevel},
		})
		logger := zap.New(core)

		maxValue := uint64(1<<64 - 1)
		logger.Info("test uint64 overflow",
			zap.Uint64("max_uint64", maxValue),
		)
		sentry.Flush(testutils.FlushTimeout())

		events := mockTransport.Events()
		require.Equal(t, 1, len(events))
		require.NotEmpty(t, events[0].Logs)

		log := events[0].Logs[0]
		maxUint64Attr, found := log.Attributes["max_uint64"]
		assert.True(t, found)
		assert.Equal(t, maxValue, maxUint64Attr.AsInterface())
	})
}

// TestTypedNilPointerValidation validates that typed nil pointers don't cause panics.
func TestTypedNilPointerValidation(t *testing.T) {
	tests := []struct {
		name  string
		field zapcore.Field
	}{
		{"Stringer", zap.Stringer("key", (*testStringer)(nil))},
		{"Error", zap.Error((*testError)(nil))},
		{"ObjectMarshaler", zap.Object("key", (*testObjectMarshaler)(nil))},
		{"ArrayMarshaler", zap.Array("key", (*testArrayMarshaler)(nil))},
		{"InlineMarshaler", zap.Inline((*testInlineMarshaler)(nil))},
	}

	for _, tt := range tests {
		t.Run("typed nil "+tt.name, func(t *testing.T) {
			entry := sentry.NewMockLogEntry()
			assert.NotPanics(t, func() {
				zapFieldToLogEntry(entry, tt.field)
			})
		})
	}

	t.Run("non-nil values still work", func(t *testing.T) {
		entry := sentry.NewMockLogEntry()

		zapFieldToLogEntry(entry, zap.Stringer("s", &testStringer{value: "test"}))
		assert.Equal(t, "test", entry.Attributes["s"])

		zapFieldToLogEntry(entry, zap.Error(errors.New("err")))
		assert.Equal(t, "err", entry.Attributes["error"])

		zapFieldToLogEntry(entry, zap.Object("o", &testObjectMarshaler{name: "test", age: 1}))
		assert.Contains(t, entry.Attributes["o"], "test")
	})
}

type customStruct struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

type testStringer struct {
	value string
}

func (t *testStringer) String() string {
	return t.value
}

type testObjectMarshaler struct {
	name string
	age  int
}

func (t *testObjectMarshaler) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("name", t.name)
	enc.AddInt("age", t.age)
	return nil
}

type testArrayMarshaler struct {
	items []string
}

func (t *testArrayMarshaler) MarshalLogArray(enc zapcore.ArrayEncoder) error {
	for _, item := range t.items {
		enc.AppendString(item)
	}
	return nil
}

type testInlineMarshaler struct {
	name  string
	score int
}

func (t *testInlineMarshaler) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("name", t.name)
	enc.AddInt("score", t.score)
	return nil
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
