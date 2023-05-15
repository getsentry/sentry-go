package sentry

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProfileCollection(t *testing.T) {
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()
	validateProfile(t, trace, elapsed)
}

func TestProfilePanicDuringStartup(t *testing.T) {
	testProfilerPanic = 1
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	trace := stopFn()
	require.Nil(t, trace)
}

func TestProfilePanicOnTick(t *testing.T) {
	testProfilerPanic = 2
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()
	validateProfile(t, trace, elapsed)
}

func doWorkFor(duration time.Duration) {
	start := time.Now()
	for time.Since(start) < duration {
		_ = findPrimeNumber(1000)
	}
}

func findPrimeNumber(n int) int {
	count := 0
	a := 2
	for count < n {
		b := 2
		prime := true // to check if found a prime
		for b*b <= a {
			if a%b == 0 {
				prime = false
				break
			}
			b++
		}
		if prime {
			count++
		}
		a++
	}
	return a - 1
}

func validateProfile(t *testing.T, trace *profileTrace, duration time.Duration) {
	var require = require.New(t)
	require.NotEmpty(trace.Samples)
	require.NotEmpty(trace.Stacks)
	require.NotEmpty(trace.Frames)
	require.NotEmpty(trace.ThreadMetadata)

	for _, sample := range trace.Samples {
		require.Greater(sample.ElapsedSinceStartNS, uint64(0))
		require.Greater(uint64(duration.Nanoseconds()), sample.ElapsedSinceStartNS)
		require.GreaterOrEqual(sample.StackID, 0)
		require.Less(sample.StackID, len(trace.Stacks))
		require.Contains(trace.ThreadMetadata, strconv.Itoa(int(sample.ThreadID)))
	}

	for _, thread := range trace.ThreadMetadata {
		require.NotEmpty(thread.Name)
	}

	for _, frame := range trace.Frames {
		require.NotEmpty(frame.Function)
		require.Greater(len(frame.AbsPath)+len(frame.Filename), 0)
		require.Greater(frame.Lineno, 0)
	}
}

func TestProfileSamplingRate(t *testing.T) {
	var require = require.New(t)

	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(500 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()

	require.NotEmpty(trace.Samples)
	var samplesByThread = map[uint64]uint64{}

	for _, sample := range trace.Samples {
		require.Greater(uint64(elapsed.Nanoseconds()), sample.ElapsedSinceStartNS)

		if prev, ok := samplesByThread[sample.ThreadID]; ok {
			// We can only verify the lower bound because the profiler callback may be scheduled less often than
			// expected, for example due to system ticker accuracy.
			// See https://stackoverflow.com/questions/70594795/more-accurate-ticker-than-time-newticker-in-go-on-macos
			// or https://github.com/golang/go/issues/44343
			require.Greater(sample.ElapsedSinceStartNS, prev)
		}
		samplesByThread[sample.ThreadID] = sample.ElapsedSinceStartNS
	}
}

func BenchmarkProfilerStartStop(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stopFn := startProfiling()
		_ = stopFn()
	}
}

func BenchmarkProfilerOnTick(b *testing.B) {
	profiler := newProfiler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiler.OnTick()
	}
}

func BenchmarkProfilerCollect(b *testing.B) {
	profiler := newProfiler()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = profiler.collectRecords()
	}
}

func BenchmarkProfilerProcess(b *testing.B) {
	profiler := newProfiler()
	records := profiler.collectRecords()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiler.processRecords(uint64(i), records)
	}
}

func doHardWork() {
	_ = findPrimeNumber(10000)
}

func BenchmarkProfilerOverheadBaseline(b *testing.B) {
	for i := 0; i < b.N; i++ {
		doHardWork()
	}
}

func BenchmarkProfilerOverheadWithProfiler(b *testing.B) {
	stopFn := startProfiling()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doHardWork()
	}
	b.StopTimer()
	stopFn()
}
