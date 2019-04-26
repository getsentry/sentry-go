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

func TestNewStacktrace(t *testing.T) {
	stacktrace := Trace()

	assertEqual(t, len(stacktrace.Frames), 3)
	assertEqual(t, stacktrace.Frames[0].Function, "TestNewStacktrace")
	assertEqual(t, stacktrace.Frames[1].Function, "Trace")
	assertEqual(t, stacktrace.Frames[2].Function, "NewStacktrace")
}

func TestStacktraceFrame(t *testing.T) {
	_, callerFile, _, _ := runtime.Caller(0)
	dir, _ := filepath.Split(callerFile)

	stacktrace := Trace()
	frame := stacktrace.Frames[len(stacktrace.Frames)-2]

	assertEqual(t, frame.Filename, "errors_test.go")
	assertEqual(t, frame.AbsPath, filepath.Join(dir, "errors_test.go"))
	assertEqual(t, frame.Function, "Trace")
	assertEqual(t, frame.Lineno, 12)
	assertEqual(t, frame.InApp, true)
	assertEqual(t, frame.Module, "sentry")
}

func TestStacktraceFrameContext(t *testing.T) {
	stacktrace := Trace()

	frame := stacktrace.Frames[len(stacktrace.Frames)-2]

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

	frame = stacktrace.Frames[len(stacktrace.Frames)-1]

	assertEqual(t, frame.PreContext, []string{
		"\tFramesOmitted [2]uint `json:\"frames_omitted,omitempty\"`",
		"}",
		"",
		"func NewStacktrace() *Stacktrace {",
		"\tpcs := make([]uintptr, 100)",
	})
	assertEqual(t, frame.ContextLine, "\tn := runtime.Callers(1, pcs)")
	assertEqual(t, frame.PostContext, []string{
		"",
		"\tif n == 0 {",
		"\t\treturn nil",
		"\t}",
		"",
	})
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
