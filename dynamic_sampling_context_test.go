package sentry

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
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
					"trace_id":   "d49d9bf66f13450b81f65bc51cf49c03",
					"public_key": "public",
					"release":    "1.0.0",
					"sampled":    "false",
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

func TestDynamicSamplingContextFromTransaction_OrgID(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		orgID   string // explicit OrgID option
		wantOrg string
	}{
		{"OrgFromDSN", "https://public@o456.ingest.sentry.io/1", "", "456"},
		{"ExplicitOrgID", "https://public@o456.ingest.sentry.io/1", "999", "999"},
		{"NoOrg", "https://public@sentry.example.com/1", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := NewTestContext(ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				Dsn:              tt.dsn,
				OrgID:            tt.orgID,
			})
			txn := StartTransaction(ctx, "test")
			dsc := DynamicSamplingContextFromTransaction(txn)

			gotOrg := dsc.Entries["org_id"]
			if gotOrg != tt.wantOrg {
				t.Errorf("expected org_id %q, got %q", tt.wantOrg, gotOrg)
			}
		})
	}
}

func TestDynamicSamplingContextFromScope_OrgID(t *testing.T) {
	dsn, _ := NewDsn("https://public@o789.ingest.sentry.io/1")
	client := &Client{
		options: ClientOptions{
			Dsn: dsn.String(),
		},
		dsn: dsn,
	}
	scope := &Scope{
		propagationContext: NewPropagationContext(),
	}

	dsc := DynamicSamplingContextFromScope(scope, client)
	if dsc.Entries["org_id"] != "789" {
		t.Errorf("expected org_id 789, got %q", dsc.Entries["org_id"])
	}

	// Test explicit OrgID override
	client.options.OrgID = "111"
	dsc = DynamicSamplingContextFromScope(scope, client)
	if dsc.Entries["org_id"] != "111" {
		t.Errorf("expected org_id 111, got %q", dsc.Entries["org_id"])
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
	}{
		"Valid input": {
			scope: &Scope{
				propagationContext: PropagationContext{
					TraceID: TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
					SpanID:  SpanIDFromHex("a9f442f9330b4e09"),
				},
			},
			client: func() *Client {
				dsn, _ := NewDsn("http://public@example.com/sentry/1")
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
		"Nil client": {
			scope: &Scope{
				propagationContext: PropagationContext{
					TraceID: TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
					SpanID:  SpanIDFromHex("a9f442f9330b4e09"),
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
			result := DynamicSamplingContextFromScope(tt.scope, tt.client)
			assertEqual(t, tt.expected, result)
		})
	}
}
