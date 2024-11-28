package sentryotel

import (
	"encoding/hex"
	"sort"
	"sync"
	"testing"
	"time"

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

// FIXME(anton): TransportMock is copied from mocks_test.go
// I don't see an easy way right now to reuse this struct in "sentry" and
// "sentryotel" packages: it naturally depends on "sentry", but tests in "sentry"
// package also depend on it, so if we move it to a new package, we'll get an
// import cycle.
// Alternatively, it could be made public on "sentry" package, but it doesn't
// feel right.

type TransportMock struct {
	mu        sync.Mutex
	events    []*sentry.Event
	lastEvent *sentry.Event
}

func (t *TransportMock) Configure(options sentry.ClientOptions) {}
func (t *TransportMock) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
	t.lastEvent = event
}
func (t *TransportMock) Flush(timeout time.Duration) bool {
	return true
}
func (t *TransportMock) Events() []*sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.events
}

func (t *TransportMock) Close()  {}

//
