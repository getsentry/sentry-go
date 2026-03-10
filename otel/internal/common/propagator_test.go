package common

import (
	"context"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestInject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dscSource   DSCSource
		spanContext *trace.SpanContext
		ctxDSC      *sentry.DynamicSamplingContext
		ctxBaggage  string
		ctxTrace    string
		wantTrace   string
		wantBaggage map[string]string
	}{
		{
			name:      "no valid span and nothing on context emits nothing",
			wantTrace: "",
		},
		{
			name:       "no valid span passes through incoming sentry-trace and baggage",
			ctxTrace:   "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
			ctxBaggage: "sentry-trace_id=abc,othervendor=val",
			wantTrace:  "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
			wantBaggage: map[string]string{
				"sentry-trace_id": "abc",
				"othervendor":     "val",
			},
		},
		{
			name:        "OTLP origin sampled emits only sentry-trace",
			spanContext: sentry.Pointer(makeSpanContext(true)),
			wantTrace:   "0102030405060708090a0b0c0d0e0f10-0102030405060708-1",
		},
		{
			name:        "OTLP origin not sampled emits sentry-trace with 0",
			spanContext: sentry.Pointer(makeSpanContext(false)),
			wantTrace:   "0102030405060708090a0b0c0d0e0f10-0102030405060708-0",
		},
		{
			name:        "downstream context DSC is forwarded as baggage",
			spanContext: sentry.Pointer(makeSpanContext(true)),
			ctxDSC: &sentry.DynamicSamplingContext{
				Entries: map[string]string{
					"trace_id":   "d4cda95b652f4a1592b449d5929fda1b",
					"public_key": "abc",
					"release":    "1.0",
					"sampled":    "true",
				},
				Frozen: true,
			},
			wantTrace: "0102030405060708090a0b0c0d0e0f10-0102030405060708-1",
			wantBaggage: map[string]string{
				"sentry-trace_id":   "d4cda95b652f4a1592b449d5929fda1b",
				"sentry-public_key": "abc",
				"sentry-release":    "1.0",
				"sentry-sampled":    "true",
			},
		},
		{
			name:        "DSC source takes priority over context DSC",
			spanContext: sentry.Pointer(makeSpanContext(true)),
			dscSource: &mockDSCSource{
				traceID: testTraceID,
				spanID:  testSpanID,
				dsc: sentry.DynamicSamplingContext{
					Entries: map[string]string{"trace_id": "fromspanmap", "sample_rate": "0.5", "sampled": "true"},
					Frozen:  true,
				},
			},
			ctxDSC: &sentry.DynamicSamplingContext{
				Entries: map[string]string{"trace_id": "fromcontext"},
				Frozen:  true,
			},
			wantTrace: "0102030405060708090a0b0c0d0e0f10-0102030405060708-1",
			wantBaggage: map[string]string{
				"sentry-trace_id":    "fromspanmap",
				"sentry-sample_rate": "0.5",
				"sentry-sampled":     "true",
			},
		},
		{
			name:        "DSC sampled=false overrides OTel sampled flag in sentry-trace",
			spanContext: sentry.Pointer(makeSpanContext(true)),
			dscSource: &mockDSCSource{
				traceID: testTraceID,
				spanID:  testSpanID,
				dsc: sentry.DynamicSamplingContext{
					Entries: map[string]string{"sampled": "false", "trace_id": "abc"},
					Frozen:  true,
				},
			},
			wantTrace: "0102030405060708090a0b0c0d0e0f10-0102030405060708-0",
			wantBaggage: map[string]string{
				"sentry-sampled":  "false",
				"sentry-trace_id": "abc",
			},
		},
		{
			name:        "DSC source miss falls back to context DSC",
			spanContext: sentry.Pointer(makeSpanContext(true)),
			dscSource: &mockDSCSource{
				traceID: trace.TraceID{0xFF},
				spanID:  trace.SpanID{0xFF},
			},
			ctxDSC: &sentry.DynamicSamplingContext{
				Entries: map[string]string{"trace_id": "fromcontext", "release": "2.0"},
				Frozen:  true,
			},
			wantTrace: "0102030405060708090a0b0c0d0e0f10-0102030405060708-1",
			wantBaggage: map[string]string{
				"sentry-trace_id": "fromcontext",
				"sentry-release":  "2.0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var opts []Option
			if tt.dscSource != nil {
				opts = append(opts, WithDSCSource(tt.dscSource))
			}
			prop := NewSentryPropagator(opts...)

			ctx := context.Background()
			if tt.spanContext != nil {
				ctx = trace.ContextWithSpanContext(ctx, *tt.spanContext)
			}
			if tt.ctxTrace != "" {
				ctx = WithSentryTraceHeader(ctx, tt.ctxTrace)
			}
			if tt.ctxBaggage != "" {
				parsed, _ := baggage.Parse(tt.ctxBaggage)
				ctx = WithBaggage(ctx, parsed)
			}
			if tt.ctxDSC != nil {
				ctx = WithDynamicSamplingContext(ctx, *tt.ctxDSC)
			}

			carrier := propagation.MapCarrier{}
			prop.Inject(ctx, carrier)

			assertEqual(t, carrier.Get(sentry.SentryTraceHeader), tt.wantTrace)
			gotBaggage := parseBaggageToMap(carrier.Get(sentry.SentryBaggageHeader))
			if diff := cmp.Diff(tt.wantBaggage, gotBaggage, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("baggage mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtract(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		sentryTrace    string
		baggageHeader  string
		wantRemote     bool
		wantTraceID    string
		wantSpanID     string
		wantDSCFrozen  bool
		wantDSCEntries map[string]string
		wantBaggageLen int
	}{
		{
			name:           "empty headers set nothing",
			wantDSCFrozen:  false,
			wantDSCEntries: nil,
		},
		{
			name:          "valid sentry-trace and baggage",
			sentryTrace:   "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1",
			baggageHeader: "sentry-trace_id=d4cda95b652f4a1592b449d5929fda1b,sentry-public_key=abc,sentry-release=1.0,othervendor=bla",
			wantRemote:    true,
			wantTraceID:   "d4cda95b652f4a1592b449d5929fda1b",
			wantSpanID:    "6e0c63257de34c92",
			wantDSCFrozen: true,
			wantDSCEntries: map[string]string{
				"trace_id":   "d4cda95b652f4a1592b449d5929fda1b",
				"public_key": "abc",
				"release":    "1.0",
			},
			wantBaggageLen: 4,
		},
		{
			name:           "only sentry-trace without baggage creates unfrozen DSC",
			sentryTrace:    "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-0",
			wantRemote:     true,
			wantTraceID:    "d4cda95b652f4a1592b449d5929fda1b",
			wantSpanID:     "6e0c63257de34c92",
			wantDSCFrozen:  false,
			wantDSCEntries: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			prop := NewSentryPropagator()
			carrier := propagation.MapCarrier{}
			if tt.sentryTrace != "" {
				carrier.Set(sentry.SentryTraceHeader, tt.sentryTrace)
			}
			if tt.baggageHeader != "" {
				carrier.Set(sentry.SentryBaggageHeader, tt.baggageHeader)
			}

			ctx := prop.Extract(context.Background(), carrier)

			assertEqual(t, TraceHeaderFromContext(ctx), tt.sentryTrace)

			sc := trace.SpanContextFromContext(ctx)
			if tt.wantRemote {
				assertEqual(t, sc.IsRemote(), true)
				assertEqual(t, sc.TraceID().String(), tt.wantTraceID)
				assertEqual(t, sc.SpanID().String(), tt.wantSpanID)
			} else {
				assertEqual(t, sc.IsValid(), false)
			}

			dsc := DynamicSamplingContextFromContext(ctx)
			assertEqual(t, dsc.Frozen, tt.wantDSCFrozen)
			if diff := cmp.Diff(tt.wantDSCEntries, dsc.Entries, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("DSC entries mismatch (-want +got):\n%s", diff)
			}

			b := BaggageFromContext(ctx)
			assertEqual(t, b.Len(), tt.wantBaggageLen)
		})
	}
}

func TestFields(t *testing.T) {
	t.Parallel()

	fields := NewSentryPropagator().Fields()
	assertEqual(t, len(fields), 2)
	assertEqual(t, fields[0], sentry.SentryTraceHeader)
	assertEqual(t, fields[1], sentry.SentryBaggageHeader)
}

// mockDSCSource returns a fixed DSC for a matching trace/span pair.
type mockDSCSource struct {
	traceID trace.TraceID
	spanID  trace.SpanID
	dsc     sentry.DynamicSamplingContext
}

func (f *mockDSCSource) GetDSC(t trace.TraceID, s trace.SpanID) (sentry.DynamicSamplingContext, bool) {
	if f != nil && t == f.traceID && s == f.spanID {
		return f.dsc, true
	}
	return sentry.DynamicSamplingContext{}, false
}

var (
	testTraceID = trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	testSpanID  = trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
)

func makeSpanContext(sampled bool) trace.SpanContext {
	flags := trace.TraceFlags(0)
	if sampled {
		flags = trace.FlagsSampled
	}
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    testTraceID,
		SpanID:     testSpanID,
		TraceFlags: flags,
	})
}

func parseBaggageToMap(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	m := make(map[string]string)
	for part := range strings.SplitSeq(raw, ",") {
		if k, v, ok := strings.Cut(part, "="); ok {
			m[k] = v
		}
	}
	return m
}
