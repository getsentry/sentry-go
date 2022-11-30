package sentry

import (
	"testing"
)

func TestDynamicSamplingContextFromHeader(t *testing.T) {
	tests := []struct {
		input []byte
		want  DynamicSamplingContext
	}{
		{
			input: []byte(""),
			want: DynamicSamplingContext{
				Frozen:  true,
				Entries: map[string]string{},
			},
		},
		{
			input: []byte("sentry-trace_id=d49d9bf66f13450b81f65bc51cf49c03,sentry-public_key=public,sentry-sample_rate=1"),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"sample_rate": "1",
				},
			},
		},
		{
			input: []byte("sentry-trace_id=d49d9bf66f13450b81f65bc51cf49c03,sentry-public_key=public,sentry-sample_rate=1,foo=bar;foo;bar;bar=baz"),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"sample_rate": "1",
				},
			},
		},
	}

	for _, tc := range tests {
		got, err := DynamicSamplingContextFromHeader(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, got, tc.want)
	}
}

func TestDynamicSamplingContextFromTransaction(t *testing.T) {
	tests := []struct {
		input *Span
		want  DynamicSamplingContext
	}{
		// Normal flow
		{
			input: func() *Span {
				ctx := NewTestContext(ClientOptions{
					EnableTracing:    true,
					TracesSampleRate: 0.5,
					Dsn:              "http://public@example.com/sentry/1",
					Release:          "1.0.0",
					Environment:      "test",
				})
				hubFromContext(ctx).ConfigureScope(func(scope *Scope) {
					scope.SetUser(User{Segment: "user_segment"})
				})
				txn := StartTransaction(ctx, "name", TransctionSource(SourceCustom))
				txn.TraceID = TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03")
				return txn
			}(),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"sample_rate":  "0.5",
					"trace_id":     "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":   "public",
					"release":      "1.0.0",
					"environment":  "test",
					"transaction":  "name",
					"user_segment": "user_segment",
				},
			},
		},
		// Transaction with source url, do not include in Dynamic Sampling context
		{
			input: func() *Span {
				ctx := NewTestContext(ClientOptions{
					EnableTracing:    true,
					TracesSampleRate: 0.5,
					Dsn:              "http://public@example.com/sentry/1",
					Release:          "1.0.0",
				})
				txn := StartTransaction(ctx, "name")
				txn.TraceID = TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03")
				return txn
			}(),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"sample_rate": "0.5",
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"release":     "1.0.0",
				},
			},
		},
	}

	for _, tc := range tests {
		got := DynamicSamplingContextFromTransaction(tc.input)
		assertEqual(t, got, tc.want)
	}
}

func TestHasEntries(t *testing.T) {
	var dsc DynamicSamplingContext

	dsc = DynamicSamplingContext{}
	assertEqual(t, dsc.HasEntries(), false)

	dsc = DynamicSamplingContext{
		Entries: map[string]string{
			"foo": "bar",
		},
	}
	assertEqual(t, dsc.HasEntries(), true)
}
