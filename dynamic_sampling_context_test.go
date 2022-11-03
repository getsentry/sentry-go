package sentry

import (
	"testing"
)

func TestNewDynamicSamplingContext(t *testing.T) {
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
		got, err := NewDynamicSamplingContext(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		assertEqual(t, got, tc.want)
	}
}
