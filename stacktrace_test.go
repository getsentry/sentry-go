package sentry

import (
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

//nolint: scopelint // false positive https://github.com/kyoh86/scopelint/issues/4
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
