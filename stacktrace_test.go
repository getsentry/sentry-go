package sentry

import (
	"runtime"
	"strings"
	"testing"
)

func trace() *Stacktrace {
	return NewStacktrace()
}

func TestFunctionName(t *testing.T) {
	for _, test := range []struct {
		skip int
		pack string
		name string
	}{
		{0, "sentry", "TestFunctionName"},
		{1, "testing", "tRunner"},
		{2, "runtime", "goexit"},
		{100, "", ""},
	} {
		pc, _, _, _ := runtime.Caller(test.skip)
		pack, name := deconstructFunctionName(runtime.FuncForPC(pc).Name())

		assertEqual(t, pack, test.pack)
		assertEqual(t, name, test.name)
	}
}

func TestStacktrace(t *testing.T) {
	stacktrace := trace()

	if stacktrace == nil {
		t.Error("got nil stacktrace")
	}

	if len(stacktrace.Frames) == 0 {
		t.Error("got zero frames")
	}
}

func TestStacktraceFrame(t *testing.T) {
	stacktrace := trace()
	frame := stacktrace.Frames[len(stacktrace.Frames)-3]
	_, callerFile, _, _ := runtime.Caller(0)

	// TODO: For now when using Go Modules, it doesnt trim anything >_>
	assertEqual(t, frame.Filename, callerFile)
	if !strings.HasSuffix(frame.AbsPath, callerFile) {
		t.Errorf("incorrect AbsPath: %s", frame.AbsPath)
	}
	assertEqual(t, frame.Function, "trace")
	assertEqual(t, frame.Lineno, 10)
	assertEqual(t, frame.InApp, true)
	assertEqual(t, frame.Module, "sentry")
}
