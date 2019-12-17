package sentry

import (
	"runtime"
	"testing"
)

func NewStacktraceForTest() *Stacktrace {
	return NewStacktrace()
}

type StacktraceTestHelper struct{}

func (StacktraceTestHelper) NewStacktrace() *Stacktrace {
	return NewStacktrace()
}

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

func BenchmarkNewStacktrace(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Trace()
	}
}
