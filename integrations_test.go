package sentry

import (
	"regexp"
	"testing"
)

func TestTransformStringsIntoRegexps(t *testing.T) {
	got := transformStringsIntoRegexps([]string{
		"+",
		"foo",
		"*",
		"(?i)bar",
		"[]",
	})

	want := []*regexp.Regexp{
		regexp.MustCompile("foo"),
		regexp.MustCompile("(?i)bar"),
	}

	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsEmptyEvent(t *testing.T) {
	event := &Event{}
	got := getIgnoreErrorsSuspects(event)
	want := []string{}
	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsMessage(t *testing.T) {
	event := &Event{
		Message: "foo",
	}
	got := getIgnoreErrorsSuspects(event)
	want := []string{"foo"}
	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsException(t *testing.T) {
	event := &Event{
		Exception: []Exception{{
			Type:  "exType",
			Value: "exVal",
		}},
	}
	got := getIgnoreErrorsSuspects(event)
	want := []string{
		"exType",
		"exVal",
	}
	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsMultipleExceptions(t *testing.T) {
	event := &Event{
		Exception: []Exception{{
			Type:  "exType",
			Value: "exVal",
		}, {
			Type:  "exTypeTwo",
			Value: "exValTwo",
		}},
	}
	got := getIgnoreErrorsSuspects(event)
	want := []string{
		"exType",
		"exVal",
		"exTypeTwo",
		"exValTwo",
	}
	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsMessageAndException(t *testing.T) {
	event := &Event{
		Message: "foo",
		Exception: []Exception{{
			Type:  "exType",
			Value: "exVal",
		}},
	}
	got := getIgnoreErrorsSuspects(event)
	want := []string{
		"foo",
		"exType",
		"exVal",
	}
	assertEqual(t, got, want)
}

func TestGetIgnoreErrorsSuspectsMessageAndMultipleExceptions(t *testing.T) {
	event := &Event{
		Message: "foo",
		Exception: []Exception{{
			Type:  "exType",
			Value: "exVal",
		}, {
			Type:  "exTypeTwo",
			Value: "exValTwo",
		}},
	}
	got := getIgnoreErrorsSuspects(event)
	want := []string{
		"foo",
		"exType",
		"exVal",
		"exTypeTwo",
		"exValTwo",
	}
	assertEqual(t, got, want)
}

func TestIgnoreErrorsIntegration(t *testing.T) {
	iei := ignoreErrorsIntegration{
		ignoreErrors: []*regexp.Regexp{
			regexp.MustCompile("foo"),
			regexp.MustCompile("(?i)bar"),
		},
	}

	dropped := &Event{
		Message: "foo",
	}

	alsoDropped := &Event{
		Exception: []Exception{{
			Type: "foo",
		}},
	}

	thisDroppedAsWell := &Event{
		Exception: []Exception{{
			Value: "Bar",
		}},
	}

	notDropped := &Event{
		Message: "dont",
	}

	alsoNotDropped := &Event{
		Exception: []Exception{{
			Type:  "really",
			Value: "dont",
		}},
	}

	if iei.processor(dropped, &EventHint{}) != nil {
		t.Error("Event should be dropped")
	}

	if iei.processor(alsoDropped, &EventHint{}) != nil {
		t.Error("Event should be dropped")
	}

	if iei.processor(thisDroppedAsWell, &EventHint{}) != nil {
		t.Error("Event should be dropped")
	}

	if iei.processor(notDropped, &EventHint{}) == nil {
		t.Error("Event should not be dropped")
	}

	if iei.processor(alsoNotDropped, &EventHint{}) == nil {
		t.Error("Event should not be dropped")
	}
}
