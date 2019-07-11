package sentry

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestFunctionName(t *testing.T) {
	for _, test := range []struct {
		skip int
		pack string
		name string
	}{
		{0, "sentry-go", "TestFunctionName"},
		{1, "testing", "tRunner"},
		{2, "runtime", "goexit"},
		{100, "", ""},
	} {
		pc, _, _, _ := runtime.Caller(test.skip)
		pack, name := deconstructFunctionName(runtime.FuncForPC(pc).Name())

		// Go1.10 reports different paths than Modules aware versions (>=1.11)
		assertStringContains(t, pack, test.pack)
		assertEqual(t, name, test.name)
	}
}

func TestNewStacktrace(t *testing.T) {
	stacktrace := Trace()

	assertEqual(t, len(stacktrace.Frames), 2)
	assertEqual(t, stacktrace.Frames[0].Function, "TestNewStacktrace")
	assertEqual(t, stacktrace.Frames[1].Function, "Trace")
}

func TestStacktraceFrame(t *testing.T) {
	_, callerFile, _, _ := runtime.Caller(0)
	dir, _ := filepath.Split(callerFile)

	stacktrace := Trace()
	frame := stacktrace.Frames[len(stacktrace.Frames)-1]

	assertEqual(t, frame.Filename, "errors_test.go")
	assertEqual(t, frame.AbsPath, filepath.Join(dir, "errors_test.go"))
	assertEqual(t, frame.Function, "Trace")
	assertEqual(t, frame.Lineno, 12)
	assertEqual(t, frame.InApp, true)
	// Go1.10 reports different Module than Modules aware versions (>=1.11)
	assertStringContains(t, frame.Module, "sentry-go")
}

// https://github.com/pkg/errors
func TestExtractStacktracePkgErrors(t *testing.T) {
	err := RedPkgErrorsRanger()
	stacktrace := ExtractStacktrace(err)

	assertEqual(t, len(stacktrace.Frames), 3)
	assertEqual(t, stacktrace.Frames[0].Function, "TestExtractStacktracePkgErrors")
	assertEqual(t, stacktrace.Frames[1].Function, "RedPkgErrorsRanger")
	assertEqual(t, stacktrace.Frames[2].Function, "BluePkgErrorsRanger")
}

// https://github.com/pingcap/errors
func TestExtractStacktracePingcapErrors(t *testing.T) {
	err := RedPingcapErrorsRanger()
	stacktrace := ExtractStacktrace(err)

	assertEqual(t, len(stacktrace.Frames), 3)
	assertEqual(t, stacktrace.Frames[0].Function, "TestExtractStacktracePingcapErrors")
	assertEqual(t, stacktrace.Frames[1].Function, "RedPingcapErrorsRanger")
	assertEqual(t, stacktrace.Frames[2].Function, "BluePingcapErrorsRanger")
}

// https://github.com/go-errors/errors
func TestExtractStacktraceGoErrors(t *testing.T) {
	err := RedGoErrorsRanger()
	stacktrace := ExtractStacktrace(err)

	assertEqual(t, len(stacktrace.Frames), 3)
	assertEqual(t, stacktrace.Frames[0].Function, "TestExtractStacktraceGoErrors")
	assertEqual(t, stacktrace.Frames[1].Function, "RedGoErrorsRanger")
	assertEqual(t, stacktrace.Frames[2].Function, "BlueGoErrorsRanger")
}
