package sentryotel

import (
	"encoding/hex"
	"sort"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var assertEqual = testutils.AssertEqual
var assertNotEqual = testutils.AssertNotEqual
var assertTrue = testutils.AssertTrue
var assertFalse = testutils.AssertFalse

// assertMapCarrierEqual compares two values of type propagation.MapCarrier and raises an
// assertion error if the values differ.
//
// It is needed because some headers (e.g. "baggage") might contain the same set of values/attributes,
// (and therefore be semantically equal), but serialized in different order.
func assertMapCarrierEqual(t *testing.T, got, want propagation.MapCarrier, userMessage ...interface{}) {
	t.Helper()

	// Make sure that keys are the same
	gotKeysSorted := got.Keys()
	sort.Strings(gotKeysSorted)
	wantKeysSorted := want.Keys()
	sort.Strings(wantKeysSorted)

	if diff := cmp.Diff(wantKeysSorted, gotKeysSorted); diff != "" {
		t.Errorf("Comparing MapCarrier keys (-want +got):\n%s", diff)
	}

	for _, key := range gotKeysSorted {
		gotValue := got.Get(key)
		wantValue := want.Get(key)

		// Ignore serialization order for baggage values
		if key == sentry.SentryBaggageHeader {
			gotBaggage, gotErr := baggage.Parse(gotValue)
			wantBaggage, wantErr := baggage.Parse(wantValue)

			if diff := cmp.Diff(wantErr, gotErr); diff != "" {
				t.Errorf("Comparing Baggage parsing errors (-want +got):\n%s", diff)
			}

			if diff := cmp.Diff(
				wantBaggage,
				gotBaggage,
				cmp.AllowUnexported(baggage.Member{}, baggage.Baggage{}),
			); diff != "" {
				t.Errorf("Comparing Baggage values (-want +got):\n%s", diff)
			}
			continue
		}

		// Everything else: do the exact comparison
		if diff := cmp.Diff(wantValue, gotValue); diff != "" {
			t.Errorf("Comparing MapCarrier values (-want +got):\n%s", diff)
		}
	}
}

// FIXME: copied from tracing_test.go
func TraceIDFromHex(s string) sentry.TraceID {
	var id sentry.TraceID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func SpanIDFromHex(s string) sentry.SpanID {
	var id sentry.SpanID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func stringPtr(s string) *string {
	return &s
}

func otelTraceIDFromHex(s string) trace.TraceID {
	if s == "" {
		return trace.TraceID{}
	}
	traceID, err := trace.TraceIDFromHex(s)
	if err != nil {
		panic(err)
	}
	return traceID
}

func otelSpanIDFromHex(s string) trace.SpanID {
	if s == "" {
		return trace.SpanID{}
	}
	spanID, err := trace.SpanIDFromHex(s)
	if err != nil {
		panic(err)
	}
	return spanID
}
