package sentry

import (
	"context"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/stretchr/testify/require"
)

func TestDynamicSamplingContextFromHeader(t *testing.T) {
	tests := []struct {
		input  []byte
		want   DynamicSamplingContext
		errMsg string
	}{
		// Empty baggage header
		{
			input: []byte(""),
			want: DynamicSamplingContext{
				Frozen:  false,
				Entries: map[string]string{},
			},
		},
		// Third-party baggage
		{
			input: []byte("other-vendor-key1=value1;value2, other-vendor-key2=value3"),
			want: DynamicSamplingContext{
				Frozen:  false,
				Entries: map[string]string{},
			},
		},
		// Sentry-only baggage
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
		// Mixed baggage
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
		// Invalid baggage value
		{
			input: []byte(","),
			want: DynamicSamplingContext{
				Frozen: false,
			},
			errMsg: "invalid baggage list-member: \"\"",
		},
	}

	for _, tc := range tests {
		got, err := DynamicSamplingContextFromHeader(tc.input)
		assertEqual(t, got, tc.want, "Context mismatch")
		if err != nil {
			assertEqual(t, err.Error(), tc.errMsg, "Error mismatch")
		}
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
					TracesSampleRate: 1.0,
					Dsn:              "http://public@example.com/sentry/1",
					Release:          "1.0.0",
					Environment:      "test",
				})
				txn := StartTransaction(ctx, "name", WithTransactionSource(SourceCustom))
				txn.TraceID = TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03")
				return txn
			}(),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"sample_rate": "1",
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"release":     "1.0.0",
					"environment": "test",
					"transaction": "name",
					"sampled":     "true",
				},
			},
		},
		// Transaction with source url, do not include in Dynamic Sampling context
		{
			input: func() *Span {
				ctx := NewTestContext(ClientOptions{
					EnableTracing:    true,
					TracesSampleRate: 0.0,
					Dsn:              "http://public@example.com/sentry/1",
					Release:          "1.0.0",
				})
				txn := StartTransaction(ctx, "name", WithTransactionSource(SourceURL))
				txn.TraceID = TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03")
				return txn
			}(),
			want: DynamicSamplingContext{
				Frozen: true,
				Entries: map[string]string{
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"release":     "1.0.0",
					"sample_rate": "0",
					"sampled":     "false",
				},
			},
		},
		// Empty context without a valid Client
		{
			input: func() *Span {
				ctx := context.Background()
				tx := StartTransaction(ctx, "op")
				return tx
			}(),
			want: DynamicSamplingContext{
				Frozen:  false,
				Entries: map[string]string{},
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

func TestString(t *testing.T) {
	var dsc DynamicSamplingContext

	dsc = DynamicSamplingContext{}
	assertEqual(t, dsc.String(), "")

	dsc = DynamicSamplingContext{
		Frozen: true,
		Entries: map[string]string{
			"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
			"public_key":  "public",
			"sample_rate": "1",
		},
	}
	testutils.AssertBaggageStringsEqual(t, dsc.String(), "sentry-trace_id=d49d9bf66f13450b81f65bc51cf49c03,sentry-public_key=public,sentry-sample_rate=1")
}

func TestDynamicSamplingContextFromScope(t *testing.T) {
	tests := map[string]struct {
		scope    *Scope
		client   *Client
		expected DynamicSamplingContext
		options  *ClientOptions
		wantRate string
	}{
		"Valid input": {
			scope: &Scope{
				scopeData: scopeData{
					propagationContext: PropagationContext{
						TraceID: TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
						SpanID:  SpanIDFromHex("a9f442f9330b4e09"),
					},
				},
			},
			client: func() *Client {
				dsn, _ := protocol.NewDsn("http://public@example.com/sentry/1")
				return &Client{
					options: ClientOptions{
						Dsn:         dsn.String(),
						Release:     "1.0.0",
						Environment: "production",
					},
					dsn: dsn,
				}
			}(),
			expected: DynamicSamplingContext{
				Entries: map[string]string{
					"trace_id":    "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key":  "public",
					"release":     "1.0.0",
					"environment": "production",
				},
				Frozen: true,
			},
		},
		"enabled static rate": {options: &ClientOptions{EnableTracing: true, TracesSampleRate: 0.25}, wantRate: "0.25"},
		"tracing disabled":    {options: &ClientOptions{TracesSampleRate: 0.25}},
		"custom sampler":      {options: &ClientOptions{EnableTracing: true, TracesSampleRate: 0.25, TracesSampler: func(SamplingContext) float64 { t.Error("scope-only DSC called a sampler"); return 1 }}},
		"invalid static rate": {options: &ClientOptions{EnableTracing: true, TracesSampleRate: -1}},
		"Nil client": {
			scope: &Scope{
				scopeData: scopeData{
					propagationContext: PropagationContext{
						TraceID: TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
						SpanID:  SpanIDFromHex("a9f442f9330b4e09"),
					},
				},
			},
			client: nil,
			expected: DynamicSamplingContext{
				Entries: map[string]string{},
				Frozen:  false,
			},
		},
		"Nil scope": {
			scope:  nil,
			client: &Client{},
			expected: DynamicSamplingContext{
				Entries: map[string]string{},
				Frozen:  false,
			},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if tt.options != nil {
				tt.scope = tests["Valid input"].scope.Clone()
				tt.scope.SetPropagationContext(PropagationContext{TraceID: TraceID{1}, SpanID: SpanID{1}})
				tt.client = &Client{options: *tt.options}
				tt.expected = DynamicSamplingContext{Frozen: true, Entries: map[string]string{traceIDContextKey: (TraceID{1}).String()}}
				if tt.wantRate != "" {
					tt.expected.Entries["sample_rate"] = tt.wantRate
				}
			}
			result := DynamicSamplingContextFromScope(tt.scope, tt.client)
			if tt.options != nil {
				require.Equal(t, SampledUndefined, tt.scope.propagationContextSnapshot().Sampled)
			}
			assertEqual(t, tt.expected, result)
		})
	}
	t.Run("first export freezes DSC across clients and clones", func(t *testing.T) {
		for _, boundary := range []string{"baggage", "event"} {
			t.Run(boundary, func(t *testing.T) {
				creator, _ := newCaptureTestClient(t, ClientOptions{EnableTracing: true, Release: "creator"})
				ctx, scope := WithIsolationScope(ContextWithClient(context.Background(), creator))
				if boundary == "baggage" {
					_ = GetBaggage(ctx)
				} else {
					require.NotNil(t, CaptureMessage(ctx, "first"))
				}
				want := scope.propagationContextSnapshot().DynamicSamplingContext
				require.True(t, want.IsFrozen())
				require.Equal(t, "creator", want.Entries["release"])
				require.Empty(t, want.Entries["transaction"])
				replacement, transport := newCaptureTestClient(t, ClientOptions{Release: "replacement"})
				for _, selected := range []*Scope{scope, scope.Clone()} {
					captureCtx := ContextWithClient(ContextWithScope(context.Background(), selected), replacement)
					assertBaggageStringsEqual(t, want.String(), GetBaggage(captureCtx))
					require.NotNil(t, CaptureMessage(captureCtx, "later"))
				}
				require.Len(t, transport.Events(), 2)
				for _, event := range transport.Events() {
					require.Equal(t, want, event.sdkMetaData.dsc)
				}
			})
		}
	})

	t.Run("frozen foreign DSC is rejected and copied", func(t *testing.T) {
		for _, test := range []struct {
			name             string
			native, matching bool
		}{
			{name: "scope foreign"},
			{name: "scope matching", matching: true},
			{name: "transaction foreign", native: true},
			{name: "transaction matching", native: true, matching: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				client, transport := newCaptureTestClient(t, ClientOptions{EnableTracing: true, TracesSampleRate: 1})
				ctx, scope := WithIsolationScope(ContextWithClient(context.Background(), client))
				propagation := scope.propagationContextSnapshot()
				traceID := propagation.TraceID
				var root *Span
				if test.native {
					root = StartTransaction(ctx, "frozen")
					ctx, traceID = root.Context(), root.TraceID
				}
				if !test.matching {
					traceID = TraceID{9}
				}
				trace := strings.ToUpper(traceID.String())
				dsc := DynamicSamplingContext{Frozen: true, Entries: map[string]string{traceIDContextKey: trace, "release": "upstream"}}
				if root == nil {
					propagation.DynamicSamplingContext = dsc
					scope.SetPropagationContext(propagation)
				} else {
					root.SetDynamicSamplingContext(dsc)
				}
				dsc.Entries["release"] = "caller mutation"
				want := DynamicSamplingContext{Frozen: true}
				if test.matching {
					want.Entries = map[string]string{traceIDContextKey: trace, "release": "upstream"}
				}
				assertBaggageStringsEqual(t, GetBaggage(ctx), want.String())
				require.NotNil(t, CaptureMessage(ctx, "frozen"))
				if root != nil {
					root.SetDynamicSamplingContext(DynamicSamplingContext{Entries: map[string]string{"release": "late"}})
					root.Finish()
				}
				for _, event := range transport.Events() {
					require.Equal(t, want, event.sdkMetaData.dsc)
				}
			})
		}
	})
}
