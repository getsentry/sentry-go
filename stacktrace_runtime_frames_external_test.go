package sentry_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/getsentry/sentry-go"
)

// runtimeFramesError mimics error types, such as samber/oops, that resolve
// their stack trace when the error is created and expose it as []runtime.Frame.
type runtimeFramesError struct {
	frames []runtime.Frame
}

func (e *runtimeFramesError) Error() string { return "this is bad from runtimeFramesError" }

func (e *runtimeFramesError) StackFrames() []runtime.Frame { return e.frames }

func captureRuntimeFramesError() error {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(1, pcs)
	iter := runtime.CallersFrames(pcs[:n])

	var frames []runtime.Frame
	for {
		frame, more := iter.Next()
		frames = append(frames, frame)
		if !more {
			break
		}
	}

	return &runtimeFramesError{frames: frames}
}

// newRuntimeFramesError is small enough to be inlined into its caller, which
// is what makes a second resolution of the frames visible.
func newRuntimeFramesError() error {
	return captureRuntimeFramesError()
}

func TestExtractStacktraceKeepsResolvedRuntimeFrames(t *testing.T) {
	err := newRuntimeFramesError()

	// Frames are innermost first, like runtime.Callers; Sentry wants the
	// opposite order and does not report the runtime and testing frames.
	var want []string
	frames := err.(*runtimeFramesError).frames
	for i := len(frames) - 1; i >= 0; i-- {
		frame := frames[i]
		if strings.HasPrefix(frame.Function, "runtime.") || strings.HasPrefix(frame.Function, "testing.") {
			continue
		}
		want = append(want, fmt.Sprintf("%s:%d", frame.Function, frame.Line))
	}

	stacktrace := sentry.ExtractStacktrace(err)
	if stacktrace == nil {
		t.Fatal("ExtractStacktrace() = nil")
	}

	var got []string
	for _, frame := range stacktrace.Frames {
		got = append(got, fmt.Sprintf("%s.%s:%d", frame.Module, frame.Function, frame.Lineno))
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("frames mismatch (-want +got):\n%s", diff)
	}
}
