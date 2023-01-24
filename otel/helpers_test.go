//go:build go1.18

package sentryotel

// TODO(anton): This is a copy of helpers_test.go in the repo root.
// We should figure out how to share testing helpers.

import (
	"encoding/hex"
	"fmt"
	"log"
	"reflect"
	"sort"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func assertEqual(t *testing.T, got, want interface{}, userMessage ...interface{}) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		logFailedAssertion(t, formatUnequalValues(got, want), userMessage...)
	}
}

func assertNotEqual(t *testing.T, got, want interface{}, userMessage ...interface{}) {
	t.Helper()

	if reflect.DeepEqual(got, want) {
		logFailedAssertion(t, formatUnequalValues(got, want), userMessage...)
	}
}

func logFailedAssertion(t *testing.T, summary string, userMessage ...interface{}) {
	t.Helper()
	text := summary

	if len(userMessage) > 0 {
		if message, ok := userMessage[0].(string); ok {
			if message != "" && len(userMessage) > 1 {
				text = fmt.Sprintf(message, userMessage[1:]...) + text
			} else if message != "" {
				text = fmt.Sprint(message) + text
			}
		}
	}

	t.Error(text)
}

func formatUnequalValues(got, want interface{}) string {
	var a, b string

	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		a, b = fmt.Sprintf("%T(%#v)", got, got), fmt.Sprintf("%T(%#v)", want, want)
	} else {
		a, b = fmt.Sprintf("%#v", got), fmt.Sprintf("%#v", want)
	}

	return fmt.Sprintf("\ngot: %s\nwant: %s", a, b)
}

// assertMapCarrierEqual compares two values of type propagation.MapCarrier and raises an
// assertion error if the values differ.
//
// It is needed because some headers (e.g. "baggage") might contain the same set of values/attributes,
// (and therefore be semantically equal), but serialized in different order.
func assertMapCarrierEqual(t *testing.T, got, want propagation.MapCarrier, userMessage ...interface{}) {
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

			// sortedBaggage = gotBaggage.Members()

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
	traceID, err := trace.TraceIDFromHex(s)
	if err != nil {
		log.Fatalf("Cannot make a TraceID from the hex string: '%s'", s)
	}
	return traceID
}

func otelSpanIDFromHex(s string) trace.SpanID {
	spanID, err := trace.SpanIDFromHex(s)
	if err != nil {
		log.Fatalf("Cannot make a SPanID from the hex string: '%s'", s)
	}
	return spanID
}
