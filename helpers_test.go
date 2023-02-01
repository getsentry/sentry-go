package sentry

import (
	"testing"

	"github.com/getsentry/sentry-go/internal/otel/baggage"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
)

var assertEqual = testutils.AssertEqual
var assertNotEqual = testutils.AssertNotEqual

func assertBaggageStringsEqual(t *testing.T, got, want string, userMessage ...interface{}) {
	t.Helper()

	baggageGot, err := baggage.Parse(got)
	if err != nil {
		t.Error(err)
	}
	baggageWant, err := baggage.Parse(want)
	if err != nil {
		t.Error(err)
	}

	if diff := cmp.Diff(
		baggageWant,
		baggageGot,
		cmp.AllowUnexported(baggage.Member{}, baggage.Baggage{}),
	); diff != "" {
		t.Errorf("Comparing Baggage (-want +got):\n%s", diff)
	}
}
