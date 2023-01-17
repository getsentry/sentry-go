package sentryotel

// TODO(anton): This is a copy of helpers_test.go in the repo root.
// We should figure out how to share testing helpers.

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
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
	assertEqual(t, gotKeysSorted, wantKeysSorted, append(userMessage, "compare MapCarrier keys")...)

	for _, key := range gotKeysSorted {
		gotValue := got.Get(key)
		wantValue := want.Get(key)

		// Ignore serialization order for baggage values
		if key == sentry.SentryBaggageHeader {
			gotBaggage, gotErr := baggage.Parse(gotValue)
			wantBaggage, wantErr := baggage.Parse(wantValue)

			assertEqual(t, gotBaggage, wantBaggage, append(userMessage, "compare Baggage values")...)
			assertEqual(t, gotErr, wantErr, append(userMessage, "compare Baggage parsing errors")...)
			continue
		}

		// Everything else: do the exact comparison
		assertEqual(t, gotValue, wantValue, userMessage...)
	}
}
