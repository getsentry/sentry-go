package sentryslog

import (
	"context"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSource(t *testing.T) {
	// Simulate a runtime frame
	pc, file, _, _ := runtime.Caller(0)
	record := &slog.Record{PC: pc}

	// Call the source function
	attr := source("sourceKey", record)

	// Assert the attributes
	assert.Equal(t, "sourceKey", attr.Key)
	assert.Equal(t, slog.KindGroup, attr.Value.Kind())

	groupAttrs := attr.Value.Group()

	expectedAttrs := map[string]any{
		"function": "github.com/getsentry/sentry-go/slog.TestSource",
		"file":     file,
		"line":     int64(15),
	}

	for _, a := range groupAttrs {
		expectedValue, ok := expectedAttrs[a.Key]
		if assert.True(t, ok, "unexpected attribute key: %s", a.Key) {
			assert.Equal(t, expectedValue, a.Value.Any())
		}
	}
}

type testLogValuer struct {
	name string
	pass string
}

func (t testLogValuer) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", t.name),
		slog.String("password", "********"),
	)
}

var stubLogValuer = testLogValuer{"userName", "password"}

func TestReplaceAttrs(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// no ReplaceAttr func
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Int("int", 42)},
		replaceAttrs(
			nil,
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Int("int", 42),
		),
	)

	// no ReplaceAttr func, but convert struct with interface slog.LogValue in slog.Group
	is.Equal(
		[]slog.Attr{slog.Group("user", slog.String("name", stubLogValuer.name), slog.String("password", "********"))},
		replaceAttrs(
			nil,
			[]string{"foobar"},
			slog.Any("user", stubLogValuer),
		),
	)

	// ReplaceAttr func, but convert struct with interface slog.LogValue in slog.Group
	is.Equal(
		[]slog.Attr{slog.Group("user", slog.String("name", stubLogValuer.name), slog.String("password", "********"))},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal([]string{"foobar", "user"}, groups)
				return a
			},
			[]string{"foobar"},
			slog.Any("user", stubLogValuer),
		),
	)

	// ReplaceAttr func, but returns the same attributes
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Int("int", 42)},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal("foobar", groups[0])
				return a
			},
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Int("int", 42),
		),
	)

	// Replace int and divide by 2
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Int("int", 21)},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal("foobar", groups[0])
				if a.Value.Kind() == slog.KindInt64 {
					a.Value = slog.Int64Value(a.Value.Int64() / 2)
				}
				return a
			},
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Int("int", 42),
		),
	)

	// Remove int attr
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Any("int", nil)},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal("foobar", groups[0])
				if a.Value.Kind() == slog.KindInt64 {
					return slog.Any("int", nil)
				}
				return a
			},
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Int("int", 42),
		),
	)

	// Rename int attr
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Int("int2", 21)},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal("foobar", groups[0])
				if a.Value.Kind() == slog.KindInt64 {
					return slog.Int("int2", 21)
				}
				return a
			},
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Int("int", 42),
		),
	)

	// Rename attr in groups
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Group("group1", slog.Group("group2", slog.Int("int", 21)))},
		replaceAttrs(
			func(groups []string, a slog.Attr) slog.Attr {
				is.Equal("foobar", groups[0])
				if len(groups) > 1 {
					is.Equal([]string{"foobar", "group1", "group2"}, groups)
					return slog.Int("int", 21)
				}
				return a
			},
			[]string{"foobar"},
			slog.Bool("bool", true), slog.Group("group1", slog.Group("group2", slog.String("string", "foobar"))),
		),
	)
}

func TestAttrsToMap(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// simple
	is.EqualValues(
		map[string]any{"key": "value"},
		attrsToMap(slog.Any("key", "value")),
	)

	// nested
	is.EqualValues(
		map[string]any{"key": "value", "key1": map[string]any{"key2": "value2"}},
		attrsToMap(slog.Any("key", "value"), slog.Group("key1", slog.Any("key2", "value2"))),
	)

	// merge
	is.EqualValues(
		map[string]any{"key": "value", "key1": map[string]any{"key2": "value2", "key3": "value3"}},
		attrsToMap(
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.Group("key1", slog.Any("key3", "value3")),
		),
	)
}

func TestExtractError(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// not found
	attrs, err := extractError(
		[]slog.Attr{
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("foo", "bar"),
		},
	)
	is.Len(attrs, 3)
	is.Nil(err)

	// found key but wrong type
	attrs, err = extractError(
		[]slog.Attr{
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("error", "bar"),
		},
	)
	is.Len(attrs, 3)
	is.Nil(err)

	// found start first key
	attrs, err = extractError(
		[]slog.Attr{
			slog.Any("error", assert.AnError),
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("foo", "bar"),
		},
	)
	is.Len(attrs, 3)
	is.EqualError(err, assert.AnError.Error())

	// found start second key
	attrs, err = extractError(
		[]slog.Attr{
			slog.Any("err", assert.AnError),
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("foo", "bar"),
		},
	)
	is.Len(attrs, 3)
	is.EqualError(err, assert.AnError.Error())

	// found middle
	attrs, err = extractError(
		[]slog.Attr{
			slog.Any("key", "value"),
			slog.Any("error", assert.AnError),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("foo", "bar"),
		},
	)
	is.Len(attrs, 3)
	is.EqualError(err, assert.AnError.Error())

	// found end
	attrs, err = extractError(
		[]slog.Attr{
			slog.Any("key", "value"),
			slog.Group("key1", slog.Any("key2", "value2")),
			slog.String("foo", "bar"),
			slog.Any("error", assert.AnError),
		},
	)
	is.Len(attrs, 3)
	is.EqualError(err, assert.AnError.Error())
}

func TestRemoveEmptyAttrs(t *testing.T) {
	t.Parallel()
	is := assert.New(t)

	// do not remove anything
	is.Equal(
		[]slog.Attr{slog.Bool("bool", true), slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Bool("bool", true), slog.Int("int", 42)},
		),
	)
	is.Equal(
		[]slog.Attr{slog.Bool("bool", false), slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Bool("bool", false), slog.Int("int", 42)},
		),
	)

	// remove if missing keys
	is.Equal(
		[]slog.Attr{slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Bool("", true), slog.Int("int", 42)},
		),
	)

	// remove if missing value
	is.Equal(
		[]slog.Attr{slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Any("test", nil), slog.Int("int", 42)},
		),
	)
	is.Equal(
		[]slog.Attr{slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Group("test"), slog.Int("int", 42)},
		),
	)

	// remove nested
	is.Equal(
		[]slog.Attr{slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Any("test", nil), slog.Int("int", 42)},
		),
	)
	is.Equal(
		[]slog.Attr{slog.Int("int", 42)},
		removeEmptyAttrs(
			[]slog.Attr{slog.Group("test", slog.Any("foobar", nil)), slog.Int("int", 42)},
		),
	)
}

func TestValueToString(t *testing.T) {
	tests := map[string]struct {
		input    slog.Attr
		expected string
	}{
		"KindInt64": {
			input:    slog.Int64("key", 42),
			expected: "42",
		},
		"KindUint64": {
			input:    slog.Uint64("key", 42),
			expected: "42",
		},
		"KindFloat64": {
			input:    slog.Float64("key", 3.14),
			expected: "3.14",
		},
		"KindString": {
			input:    slog.String("key", "test"),
			expected: "test",
		},
		"KindBool": {
			input:    slog.Bool("key", true),
			expected: "true",
		},
		"KindDuration": {
			input:    slog.Duration("key", time.Second*42),
			expected: "42s",
		},
		"KindTime": {
			input:    slog.Time("key", time.Date(2023, time.July, 30, 12, 0, 0, 0, time.UTC)),
			expected: "2023-07-30 12:00:00 +0000 UTC",
		},
		"KindAny": {
			input:    slog.Any("key", "any value"),
			expected: "any value",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual := valueToString(tc.input.Value)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

type ctxKey string

func TestContextExtractor(t *testing.T) {
	tests := map[string]struct {
		ctx      context.Context
		fns      []func(ctx context.Context) []slog.Attr
		expected []slog.Attr
	}{
		"NoFunctions": {
			ctx:      context.Background(),
			fns:      []func(ctx context.Context) []slog.Attr{},
			expected: []slog.Attr{},
		},
		"SingleFunction": {
			ctx: context.Background(),
			fns: []func(ctx context.Context) []slog.Attr{
				func(ctx context.Context) []slog.Attr {
					return []slog.Attr{slog.String("key1", "value1")}
				},
			},
			expected: []slog.Attr{slog.String("key1", "value1")},
		},
		"MultipleFunctions": {
			ctx: context.Background(),
			fns: []func(ctx context.Context) []slog.Attr{
				func(ctx context.Context) []slog.Attr {
					return []slog.Attr{slog.String("key1", "value1")}
				},
				func(ctx context.Context) []slog.Attr {
					return []slog.Attr{slog.String("key2", "value2")}
				},
			},
			expected: []slog.Attr{slog.String("key1", "value1"), slog.String("key2", "value2")},
		},
		"FunctionWithContext": {
			ctx: context.WithValue(context.Background(), ctxKey("userID"), "1234"),
			fns: []func(ctx context.Context) []slog.Attr{
				func(ctx context.Context) []slog.Attr {
					if userID, ok := ctx.Value(ctxKey("userID")).(string); ok {
						return []slog.Attr{slog.String("userID", userID)}
					}
					return []slog.Attr{}
				},
			},
			expected: []slog.Attr{slog.String("userID", "1234")},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual := contextExtractor(tc.ctx, tc.fns)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestAppendAttrsToGroup(t *testing.T) {
	tests := map[string]struct {
		groups      []string
		actualAttrs []slog.Attr
		newAttrs    []slog.Attr
		expected    []slog.Attr
	}{
		"NoGroups": {
			groups:      []string{},
			actualAttrs: []slog.Attr{slog.String("key1", "value1")},
			newAttrs:    []slog.Attr{slog.String("key2", "value2")},
			expected:    []slog.Attr{slog.String("key1", "value1"), slog.String("key2", "value2")},
		},
		"SingleGroup": {
			groups: []string{"group1"},
			actualAttrs: []slog.Attr{
				slog.Group("group1", slog.String("key1", "value1")),
			},
			newAttrs: []slog.Attr{slog.String("key2", "value2")},
			expected: []slog.Attr{
				slog.Group("group1", slog.String("key1", "value1"), slog.String("key2", "value2")),
			},
		},
		"NestedGroups": {
			groups: []string{"group1", "group2"},
			actualAttrs: []slog.Attr{
				slog.Group("group1", slog.Group("group2", slog.String("key1", "value1"))),
			},
			newAttrs: []slog.Attr{slog.String("key2", "value2")},
			expected: []slog.Attr{
				slog.Group("group1", slog.Group("group2", slog.String("key1", "value1"), slog.String("key2", "value2"))),
			},
		},
		"NewGroup": {
			groups:      []string{"group1"},
			actualAttrs: []slog.Attr{slog.String("key1", "value1")},
			newAttrs:    []slog.Attr{slog.String("key2", "value2")},
			expected: []slog.Attr{
				slog.String("key1", "value1"),
				slog.Group("group1", slog.String("key2", "value2")),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual := appendAttrsToGroup(tc.groups, tc.actualAttrs, tc.newAttrs...)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestToAnySlice(t *testing.T) {
	tests := map[string]struct {
		input    []slog.Attr
		expected []any
	}{
		"EmptySlice": {
			input:    []slog.Attr{},
			expected: []any{},
		},
		"SingleElement": {
			input:    []slog.Attr{slog.String("key1", "value1")},
			expected: []any{slog.String("key1", "value1")},
		},
		"MultipleElements": {
			input: []slog.Attr{
				slog.String("key1", "value1"),
				slog.Int("key2", 2),
				slog.Bool("key3", true),
			},
			expected: []any{
				slog.String("key1", "value1"),
				slog.Int("key2", 2),
				slog.Bool("key3", true),
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			actual := toAnySlice(tc.input)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

type textMarshalerExample struct {
	Data string
}

func (t textMarshalerExample) MarshalText() (text []byte, err error) {
	return []byte(t.Data), nil
}

type nonTextMarshalerExample struct {
	Data string
}

func TestAnyValueToString(t *testing.T) {
	tests := map[string]struct {
		input    slog.Attr
		expected string
	}{
		"TextMarshaler implementation": {
			input:    slog.Any("key", textMarshalerExample{Data: "example"}),
			expected: "example",
		},
		"Non-TextMarshaler implementation": {
			input:    slog.Any("key", nonTextMarshalerExample{Data: "example"}),
			expected: "{Data:example}",
		},
		"String value": {
			input:    slog.String("key", "example string"),
			expected: "example string",
		},
		"Integer value": {
			input:    slog.Int("key", 42),
			expected: "42",
		},
		"Boolean value": {
			input:    slog.Bool("key", true),
			expected: "true",
		},
		"Nil value": {
			input:    slog.Any("key", nil),
			expected: "<nil>",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			output := anyValueToString(tt.input.Value)
			if output != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, output)
			}
		})
	}
}
