package sentry

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func NewStacktraceForTest() *Stacktrace {
	return NewStacktrace()
}

type StacktraceTestHelper struct{}

func (StacktraceTestHelper) NewStacktrace() *Stacktrace {
	return NewStacktrace()
}

func BenchmarkNewStacktrace(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Trace()
	}
}

// nolint: scopelint // false positive https://github.com/kyoh86/scopelint/issues/4
func TestSplitQualifiedFunctionName(t *testing.T) {
	tests := []struct {
		in  string
		pkg string
		fun string
	}{
		{"", "", ""},
		{"runtime.Callers", "runtime", "Callers"},
		{"main.main.func1", "main", "main.func1"},
		{
			"github.com/getsentry/sentry-go.Init",
			"github.com/getsentry/sentry-go",
			"Init",
		},
		{
			"github.com/getsentry/sentry-go.(*Hub).Flush",
			"github.com/getsentry/sentry-go",
			"(*Hub).Flush",
		},
		{
			"github.com/getsentry/sentry-go.Test.func2.1.1",
			"github.com/getsentry/sentry-go",
			"Test.func2.1.1",
		},
		{
			"github.com/getsentry/confusing%2epkg%2ewith%2edots.Test.func1",
			"github.com/getsentry/confusing%2epkg%2ewith%2edots",
			"Test.func1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			pkg, fun := splitQualifiedFunctionName(tt.in)
			if diff := cmp.Diff(tt.pkg, pkg); diff != "" {
				t.Errorf("Package name mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.fun, fun); diff != "" {
				t.Errorf("Function name mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// nolint: scopelint // false positive https://github.com/kyoh86/scopelint/issues/4
func TestFilterFrames(t *testing.T) {
	tests := []struct {
		in  []Frame
		out []Frame
	}{
		// sanity check
		{},
		// filter out go internals and SDK internals; "sentry-go_test" is
		// considered outside of the SDK and thus included (useful for testing)
		{
			in: []Frame{
				{
					Function: "goexit",
					Module:   "runtime",
					AbsPath:  "/goroot/src/runtime/asm_amd64.s",
					InApp:    false,
				},
				{
					Function: "tRunner",
					Module:   "testing",
					AbsPath:  "/goroot/src/testing/testing.go",
					InApp:    false,
				},
				{
					Function: "TestNewStacktrace.func1",
					Module:   "github.com/getsentry/sentry-go_test",
					AbsPath:  "/somewhere/sentry/sentry-go/stacktrace_external_test.go",
					InApp:    true,
				},
				{
					Function: "StacktraceTestHelper.NewStacktrace",
					Module:   "github.com/getsentry/sentry-go",
					AbsPath:  "/somewhere/sentry/sentry-go/stacktrace_test.go",
					InApp:    true,
				},
				{
					Function: "NewStacktrace",
					Module:   "github.com/getsentry/sentry-go",
					AbsPath:  "/somewhere/sentry/sentry-go/stacktrace.go",
					InApp:    true,
				},
			},
			out: []Frame{
				{
					Function: "TestNewStacktrace.func1",
					Module:   "github.com/getsentry/sentry-go_test",
					AbsPath:  "/somewhere/sentry/sentry-go/stacktrace_external_test.go",
					InApp:    true,
				},
			},
		},
		// filter out integrations; SDK subpackages
		{
			in: []Frame{
				{
					Function: "Example.Integration",
					Module:   "github.com/getsentry/sentry-go/http/integration",
					AbsPath:  "/somewhere/sentry/sentry-go/http/integration/integration.go",
					InApp:    true,
				},
				{
					Function: "(*Handler).Handle",
					Module:   "github.com/getsentry/sentry-go/http",
					AbsPath:  "/somewhere/sentry/sentry-go/http/sentryhttp.go",
					InApp:    true,
				},
				{
					Function: "main",
					Module:   "main",
					AbsPath:  "/somewhere/example.com/pkg/main.go",
					InApp:    true,
				},
			},
			out: []Frame{
				{
					Function: "main",
					Module:   "main",
					AbsPath:  "/somewhere/example.com/pkg/main.go",
					InApp:    true,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := filterFrames(tt.in)
			if diff := cmp.Diff(tt.out, got); diff != "" {
				t.Errorf("filterFrames() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestExtractXErrorsPC(t *testing.T) {
	// This ensures that extractXErrorsPC does not break code that doesn't use
	// golang.org/x/xerrors. For tests that check that it works on the
	// appropriate type of errors, see stacktrace_external_test.go.
	if got := extractXErrorsPC(errors.New("test")); got != nil {
		t.Errorf("got %#v, want nil", got)
	}
}
