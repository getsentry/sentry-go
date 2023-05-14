package traceparser

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseEmpty(t *testing.T) {
	var require = require.New(t)

	require.Zero(Parse(nil).Length)
	require.Zero(Parse([]byte{}).Length)
}

var tracetext = []byte(`
goroutine 18 [running]:
testing.(*M).startAlarm.func1()
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2241 +0x3c5
created by time.goFunc
	C:/Users/name/scoop/apps/go/current/src/time/sleep.go:176 +0x32

goroutine 1 [chan receive]:
testing.(*T).Run(0xc00006f6c0, {0x672288?, 0x180fd3?}, 0x6b5f98)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1630 +0x405
testing.runTests.func1(0xa36e00?)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2036 +0x45
testing.tRunner(0xc00006f6c0, 0xc0000b3c88)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1576 +0x10b
testing.runTests(0xc000035ea0?, {0xa31240, 0xcd, 0xcd}, {0xc0000befa0?, 0x102df4ae6c418?, 0xa363a0?})
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2034 +0x489
testing.(*M).Run(0xc000035ea0)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1906 +0x63a
main.main()
	_testmain.go:465 +0x1aa

goroutine 6 [chan send]:
github.com/getsentry/sentry-go.startProfiling.func3()
	c:/dev/sentry-go/profiler.go:46 +0x2b
github.com/getsentry/sentry-go.TestStart(0x0?)
	c:/dev/sentry-go/profiler_test.go:13 +0x3e
testing.tRunner(0xc00006f860, 0x6b5f98)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1576 +0x10b
created by testing.(*T).Run
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1629 +0x3ea

goroutine 7 [stopping the world]:
runtime.Stack({0xc000200000, 0x100000, 0x100000}, 0x1)
	C:/Users/name/scoop/apps/go/current/src/runtime/mprof.go:1193 +0x4d
github.com/getsentry/sentry-go.(*profileRecorder).Collect(0xc00008a820)
	c:/dev/sentry-go/profiler.go:73 +0x3b
github.com/getsentry/sentry-go.startProfiling.func2()
	c:/dev/sentry-go/profiler.go:38 +0xb1
created by github.com/getsentry/sentry-go.startProfiling
	c:/dev/sentry-go/profiler.go:31 +0x36c

goroutine 19 [chan send]:
github.com/getsentry/sentry-go.startProfiling.func1()
	c:/dev/sentry-go/profiler.go:29 +0x25
...additional frames elided...
created by time.goFunc
	C:/Users/name/scoop/apps/go/current/src/time/sleep.go:176 +0x32
`)

func TestParse(t *testing.T) {
	var require = require.New(t)

	var traces = Parse(tracetext)
	var i = 0
	var checkTrace = func(id int, stack string) {
		var trace = traces.Item(i)
		require.NotNil(trace)
		require.Equal(uint64(id), trace.GoID())
		require.Equal(stack, string(trace.UniqueIdentifier()))
		i++
	}

	checkTrace(18, `testing.(*M).startAlarm.func1()
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2241 +0x3c5
created by time.goFunc
	C:/Users/name/scoop/apps/go/current/src/time/sleep.go:176 +0x32`)

	checkTrace(1, `testing.(*T).Run(0xc00006f6c0, {0x672288?, 0x180fd3?}, 0x6b5f98)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1630 +0x405
testing.runTests.func1(0xa36e00?)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2036 +0x45
testing.tRunner(0xc00006f6c0, 0xc0000b3c88)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1576 +0x10b
testing.runTests(0xc000035ea0?, {0xa31240, 0xcd, 0xcd}, {0xc0000befa0?, 0x102df4ae6c418?, 0xa363a0?})
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:2034 +0x489
testing.(*M).Run(0xc000035ea0)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1906 +0x63a
main.main()
	_testmain.go:465 +0x1aa`)

	checkTrace(6, `github.com/getsentry/sentry-go.startProfiling.func3()
	c:/dev/sentry-go/profiler.go:46 +0x2b
github.com/getsentry/sentry-go.TestStart(0x0?)
	c:/dev/sentry-go/profiler_test.go:13 +0x3e
testing.tRunner(0xc00006f860, 0x6b5f98)
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1576 +0x10b
created by testing.(*T).Run
	C:/Users/name/scoop/apps/go/current/src/testing/testing.go:1629 +0x3ea`)

	checkTrace(7, `runtime.Stack({0xc000200000, 0x100000, 0x100000}, 0x1)
	C:/Users/name/scoop/apps/go/current/src/runtime/mprof.go:1193 +0x4d
github.com/getsentry/sentry-go.(*profileRecorder).Collect(0xc00008a820)
	c:/dev/sentry-go/profiler.go:73 +0x3b
github.com/getsentry/sentry-go.startProfiling.func2()
	c:/dev/sentry-go/profiler.go:38 +0xb1
created by github.com/getsentry/sentry-go.startProfiling
	c:/dev/sentry-go/profiler.go:31 +0x36c`)

	checkTrace(19, `github.com/getsentry/sentry-go.startProfiling.func1()
	c:/dev/sentry-go/profiler.go:29 +0x25
...additional frames elided...
created by time.goFunc
	C:/Users/name/scoop/apps/go/current/src/time/sleep.go:176 +0x32`)

	require.Equal(traces.Length, i)
}

func TestFrames(t *testing.T) {
	var require = require.New(t)

	var output = ""
	var traces = Parse(tracetext)
	for i := 0; i < traces.Length; i++ {
		var trace = traces.Item(i)
		var framesIter = trace.FramesReversed()
		output += fmt.Sprintf("Trace %d: goroutine %d with at most %d frames\n", i, trace.GoID(), framesIter.LengthUpperBound())

		for framesIter.HasNext() {
			var frame = framesIter.Next()
			output += fmt.Sprintf("  Func = %s\n", frame.Func())
			file, line := frame.File()
			output += fmt.Sprintf("  File = %s\n", file)
			output += fmt.Sprintf("  Line = %d\n", line)
		}
	}

	var expected = strings.Split(strings.TrimLeft(`
Trace 0: goroutine 18 with at most 2 frames
  Func = time.goFunc
  File = C:/Users/name/scoop/apps/go/current/src/time/sleep.go
  Line = 176
  Func = testing.(*M).startAlarm.func1
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 2241
Trace 1: goroutine 1 with at most 6 frames
  Func = main.main
  File = _testmain.go
  Line = 465
  Func = testing.(*M).Run
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 1906
  Func = testing.runTests
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 2034
  Func = testing.tRunner
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 1576
  Func = testing.runTests.func1
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 2036
  Func = testing.(*T).Run
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 1630
Trace 2: goroutine 6 with at most 4 frames
  Func = testing.(*T).Run
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 1629
  Func = testing.tRunner
  File = C:/Users/name/scoop/apps/go/current/src/testing/testing.go
  Line = 1576
  Func = github.com/getsentry/sentry-go.TestStart
  File = c:/dev/sentry-go/profiler_test.go
  Line = 13
  Func = github.com/getsentry/sentry-go.startProfiling.func3
  File = c:/dev/sentry-go/profiler.go
  Line = 46
Trace 3: goroutine 7 with at most 4 frames
  Func = github.com/getsentry/sentry-go.startProfiling
  File = c:/dev/sentry-go/profiler.go
  Line = 31
  Func = github.com/getsentry/sentry-go.startProfiling.func2
  File = c:/dev/sentry-go/profiler.go
  Line = 38
  Func = github.com/getsentry/sentry-go.(*profileRecorder).Collect
  File = c:/dev/sentry-go/profiler.go
  Line = 73
  Func = runtime.Stack
  File = C:/Users/name/scoop/apps/go/current/src/runtime/mprof.go
  Line = 1193
Trace 4: goroutine 19 with at most 2 frames
  Func = time.goFunc
  File = C:/Users/name/scoop/apps/go/current/src/time/sleep.go
  Line = 176
  Func = github.com/getsentry/sentry-go.startProfiling.func1
  File = c:/dev/sentry-go/profiler.go
  Line = 29
`, "\n"), "\n")
	require.Equal(expected, strings.Split(output, "\n"))
}

func BenchmarkEqualBytes(b *testing.B) {
	lines := bytes.Split(tracetext, lineSeparator)
	var framesElided = []byte(framesElided)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for n := 0; n < len(lines); n++ {
			if bytes.Equal(lines[n], framesElided) {
				break
			}
		}
	}
}

// fastest
func BenchmarkStringEqual(b *testing.B) {
	lines := bytes.Split(tracetext, lineSeparator)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for n := 0; n < len(lines); n++ {
			if string(lines[n]) == framesElided {
				break
			}
		}
	}
}

func BenchmarkEqualPrefix(b *testing.B) {
	lines := bytes.Split(tracetext, lineSeparator)
	var framesElided = []byte(framesElided)
	var ln = len(framesElided)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for n := 0; n < len(lines); n++ {
			if len(lines[n]) == ln && bytes.HasPrefix(lines[n], framesElided) {
				break
			}
		}
	}
}

func BenchmarkFullParse(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	var start = time.Now()
	var parsedBytes = len(tracetext) * b.N
	for i := 0; i < b.N; i++ {
		var traces = Parse(tracetext)
		for i := 0; i < traces.Length; i++ {
			var trace = traces.Item(i)
			_ = trace.GoID()

			var iter = trace.FramesReversed()
			_ = iter.LengthUpperBound()
			for iter.HasNext() {
				var frame = iter.Next()
				_ = frame.Func()
				_, _ = frame.File()
			}
		}
	}

	var throughput = float64(parsedBytes) / time.Since(start).Seconds() / 1024 / 1024
	b.ReportMetric(throughput, "MiB/s")
}

func BenchmarkSplitOnly(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()

	var start = time.Now()
	var parsedBytes = len(tracetext) * b.N
	for i := 0; i < b.N; i++ {
		var traces = Parse(tracetext)
		for i := 0; i < traces.Length; i++ {
			var trace = traces.Item(i)
			_ = trace.UniqueIdentifier()
		}
	}

	var throughput = float64(parsedBytes) / time.Since(start).Seconds() / 1024 / 1024
	b.ReportMetric(throughput, "MiB/s")
}
