package sentry

import (
	"path/filepath"
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

	if event := iei.processor(dropped, &EventHint{}, Logger); event != nil {
		t.Error("Event should be dropped")
	}

	if event := iei.processor(alsoDropped, &EventHint{}, Logger); event != nil {
		t.Error("Event should be dropped")
	}

	if event := iei.processor(thisDroppedAsWell, &EventHint{}, Logger); event != nil {
		t.Error("Event should be dropped")
	}

	if event := iei.processor(notDropped, &EventHint{}, Logger); event == nil {
		t.Error("Event should not be dropped")
	}

	if event := iei.processor(alsoNotDropped, &EventHint{}, Logger); event == nil {
		t.Error("Event should not be dropped")
	}
}

func TestContextifyFrames(t *testing.T) {
	cfi := contextifyFramesIntegration{
		sr:           newSourceReader(),
		contextLines: 5,
	}

	filename := "errors_test.go"
	abspath, err := filepath.Abs("errors_test.go")
	if err != nil {
		t.Fatal(err)
	}

	frames := cfi.contextify([]Frame{{
		Function: "Trace",
		Module:   "github.com/getsentry/sentry-go",
		Filename: filename,
		AbsPath:  abspath,
		Lineno:   12,
		InApp:    true,
	}})
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	frame := frames[0]

	assertEqual(t, frame.PreContext, []string{
		")",
		"",
		"// NOTE: if you modify this file, you are also responsible for updating LoC position in Stacktrace tests",
		"",
		"func Trace() *Stacktrace {",
	})
	assertEqual(t, frame.ContextLine, "\treturn NewStacktrace()")
	assertEqual(t, frame.PostContext, []string{
		"}",
		"",
		"func RedPkgErrorsRanger() error {",
		"\treturn BluePkgErrorsRanger()",
		"}",
	})
}

func TestContextifyFramesNonexistingFilesShouldNotDropFrames(t *testing.T) {
	cfi := contextifyFramesIntegration{
		sr:           newSourceReader(),
		contextLines: 5,
	}

	frames := []Frame{{
		InApp:    true,
		Function: "fnName",
		Module:   "same",
		Filename: "wat.go",
		AbsPath:  "this/doesnt/exist/wat.go",
		Lineno:   1,
		Colno:    2,
	}, {
		InApp:    false,
		Function: "fnNameFoo",
		Module:   "sameFoo",
		Filename: "foo.go",
		AbsPath:  "this/doesnt/exist/foo.go",
		Lineno:   3,
		Colno:    5,
	}}

	contextifiedFrames := cfi.contextify(frames)
	assertEqual(t, len(contextifiedFrames), len(frames))
}
