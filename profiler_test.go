package sentry

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProfilerCollection(t *testing.T) {
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()
	validateProfile(t, trace, elapsed)
}

func TestProfilerCollectsOnStart(t *testing.T) {
	start := time.Now()
	trace := startProfiling()()
	validateProfile(t, trace, time.Since(start))
}

func TestProfilerPanicDuringStartup(t *testing.T) {
	testProfilerPanic = -1
	testProfilerPanickedWith = nil
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	trace := stopFn()
	require.Nil(t, trace)
	require.Equal(t, "This is an expected panic in profilerGoroutine() during tests", testProfilerPanickedWith.(string))
}

func TestProfilerPanicOnTick(t *testing.T) {
	testProfilerPanic = 10_000
	testProfilerPanickedWith = nil
	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(35 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()
	require.Equal(t, "This is an expected panic in Profiler.OnTick() during tests", testProfilerPanickedWith.(string))
	validateProfile(t, trace, elapsed)
}

func TestProfilerPanicOnTickDirect(t *testing.T) {
	var require = require.New(t)

	testProfilerPanic = 1
	profiler := newProfiler()
	time.Sleep(time.Millisecond)
	// This is handled by the profiler goroutine and stops the profiler.
	require.Panics(profiler.OnTick)
	require.Empty(profiler.trace.Samples)

	profiler.OnTick()
	require.NotEmpty(profiler.trace.Samples)
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
		require.GreaterOrEqual(sample.ElapsedSinceStartNS, uint64(0))
		require.GreaterOrEqual(uint64(duration.Nanoseconds()), sample.ElapsedSinceStartNS)
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

func TestProfilerSamplingRate(t *testing.T) {
	var require = require.New(t)

	start := time.Now()
	stopFn := startProfiling()
	doWorkFor(500 * time.Millisecond)
	elapsed := time.Since(start)
	trace := stopFn()

	require.NotEmpty(trace.Samples)
	var samplesByThread = map[uint64]uint64{}

	for _, sample := range trace.Samples {
		require.GreaterOrEqual(uint64(elapsed.Nanoseconds()), sample.ElapsedSinceStartNS)

		if prev, ok := samplesByThread[sample.ThreadID]; ok {
			// We can only verify the lower bound because the profiler callback may be scheduled less often than
			// expected, for example due to system ticker accuracy.
			// See https://stackoverflow.com/questions/70594795/more-accurate-ticker-than-time-newticker-in-go-on-macos
			// or https://github.com/golang/go/issues/44343
			require.Greater(sample.ElapsedSinceStartNS, prev)
		} else {
			// First sample should come in before the defined sampling rate.
			require.Less(sample.ElapsedSinceStartNS, uint64(profilerSamplingRate.Nanoseconds()))
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
