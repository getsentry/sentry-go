package common

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"github.com/getsentry/sentry-go/internal/testutils"
)

var assertEqual = testutils.AssertEqual

func TestDynamicSamplingContextFromContext(t *testing.T) {
	t.Run("returns zero value when not set", func(t *testing.T) {
		dsc := DynamicSamplingContextFromContext(context.Background())
		assertEqual(t, dsc.HasEntries(), false)
	})

	t.Run("round-trips correctly", func(t *testing.T) {
		dsc := sentry.DynamicSamplingContext{
			Entries: map[string]string{"trace_id": "abc123", "public_key": "key"},
			Frozen:  true,
		}
		ctx := WithDynamicSamplingContext(context.Background(), dsc)
		got := DynamicSamplingContextFromContext(ctx)
		assertEqual(t, got, dsc)
	})
}

func TestSentryTraceHeaderFromContext(t *testing.T) {
	t.Run("returns empty string when not set", func(t *testing.T) {
		header := TraceHeaderFromContext(context.Background())
		assertEqual(t, header, "")
	})

	t.Run("round-trips correctly", func(t *testing.T) {
		ctx := WithSentryTraceHeader(context.Background(), "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1")
		got := TraceHeaderFromContext(ctx)
		assertEqual(t, got, "d4cda95b652f4a1592b449d5929fda1b-6e0c63257de34c92-1")
	})
}

func TestTraceParentContextFromContext(t *testing.T) {
	t.Run("returns undefined sampled when not set", func(t *testing.T) {
		tpc := TraceParentContextFromContext(context.Background())
		assertEqual(t, tpc.Sampled, sentry.SampledUndefined)
	})

	t.Run("round-trips correctly", func(t *testing.T) {
		tpc := sentry.TraceParentContext{Sampled: sentry.SampledTrue}
		ctx := WithTraceParentContext(context.Background(), tpc)
		got := TraceParentContextFromContext(ctx)
		assertEqual(t, got.Sampled, sentry.SampledTrue)
	})
}

func TestBaggageFromContext(t *testing.T) {
	t.Run("returns empty baggage when not set", func(t *testing.T) {
		b := BaggageFromContext(context.Background())
		assertEqual(t, b.Len(), 0)
	})

	t.Run("round-trips correctly", func(t *testing.T) {
		b, _ := baggage.Parse("key1=value1,key2=value2")
		ctx := WithBaggage(context.Background(), b)
		got := BaggageFromContext(ctx)
		assertEqual(t, got.Len(), 2)
	})
}
