package sentry

import (
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProfilerCollection(t *testing.T) {
	start := time.Now()
	stopFn := startProfiling(start)
	doWorkFor(35 * time.Millisecond)
	result := stopFn()
	elapsed := time.Since(start)
	require.NotNil(t, result)
	require.Greater(t, result.callerGoID, uint64(0))
	validateProfile(t, result.trace, elapsed)
}

func TestProfilerCollectsOnStart(t *testing.T) {
	start := time.Now()
	result := startProfiling(start)()
	require.NotNil(t, result)
	validateProfile(t, result.trace, time.Since(start))
}

func workUntilProfilerPanicked() {
	for i := 0; i < 100 && atomic.LoadInt64(&testProfilerPanic) != 0; i++ {
		doWorkFor(10 * time.Millisecond)
	}
}

func TestProfilerPanicDuringStartup(t *testing.T) {
	var require = require.New(t)

	atomic.StoreInt64(&testProfilerPanic, -1)

	stopFn := startProfiling(time.Now())
	workUntilProfilerPanicked()
	result := stopFn()

	require.Zero(atomic.LoadInt64(&testProfilerPanic))
	require.Nil(result)
}

func TestProfilerPanicOnTick(t *testing.T) {
	var require = require.New(t)

	// Panic after the first sample is collected.
	atomic.StoreInt64(&testProfilerPanic, profilerSamplingRate.Nanoseconds())

	start := time.Now()
	stopFn := startProfiling(start)
	workUntilProfilerPanicked()
	result := stopFn()
	elapsed := time.Since(start)

	require.Zero(atomic.LoadInt64(&testProfilerPanic))
	require.NotNil(result)
	validateProfile(t, result.trace, elapsed)
}

func TestProfilerPanicOnTickDirect(t *testing.T) {
	var require = require.New(t)

	atomic.StoreInt64(&testProfilerPanic, 1)

	profiler := newProfiler(time.Now())

	time.Sleep(time.Millisecond)

	// This is normally handled by the profiler goroutine and stops the profiler.
	require.Panics(profiler.onTick)
	require.Empty(profiler.trace.Samples)

	require.Equal(int64(1), atomic.LoadInt64(&testProfilerPanic))
	atomic.StoreInt64(&testProfilerPanic, 0)

	profiler.onTick()
	require.NotEmpty(profiler.trace.Samples)
}

func doWorkFor(duration time.Duration) {
	start := time.Now()
	for time.Since(start) < duration {
		_ = findPrimeNumber(1000)
		runtime.Gosched()
	}
}

//nolint:unparam
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
	require.NotNil(trace)
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

	stopFn := startProfiling(time.Now())
	doWorkFor(500 * time.Millisecond)
	result := stopFn()

	require.NotEmpty(result.trace.Samples)
	var samplesByThread = map[uint64]uint64{}
	var outliersByThread = map[uint64]uint64{}
	var outliers = 0
	for _, sample := range result.trace.Samples {
		count := samplesByThread[sample.ThreadID]

		var lowerBound = count * uint64(profilerSamplingRate.Nanoseconds())
		var upperBound = (count + 1 + outliersByThread[sample.ThreadID]) * uint64(profilerSamplingRate.Nanoseconds())

		t.Logf("Routine %2d, sample %d (%d) should be between %d and %d", sample.ThreadID, count, sample.ElapsedSinceStartNS, lowerBound, upperBound)

		// We can check the lower bound explicitly, but the upper bound is problematic as some samples may get delayed.
		// Therefore, we collect the number of outliers and check if it's reasonably low.
		require.GreaterOrEqual(sample.ElapsedSinceStartNS, lowerBound)
		if sample.ElapsedSinceStartNS > upperBound {
			// We also increase the count by one to shift the followup samples too.
			outliersByThread[sample.ThreadID]++
			if int(outliersByThread[sample.ThreadID]) > outliers {
				outliers = int(outliersByThread[sample.ThreadID])
			}
		}

		samplesByThread[sample.ThreadID] = count + 1
	}

	require.Less(outliers, len(result.trace.Samples)/10)
}

func testTick(t *testing.T, count, i int, prevTick time.Time) time.Time {
	var sinceLastTick = time.Since(prevTick).Microseconds()
	t.Logf("tick %2d/%d after %d μs", i+1, count, sinceLastTick)
	return time.Now()
}

// This test measures the accuracy of time.NewTicker() on the current system.
func TestProfilerTimeTicker(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping on CI because the machines are too overloaded to provide consistent ticker resolution.")
	}

	onProfilerStart() // This fixes Windows ticker resolution.

	t.Logf("We're expecting a tick once every %d μs", profilerSamplingRate.Microseconds())

	var startTime = time.Now()
	var ticker = time.NewTicker(profilerSamplingRate)
	defer ticker.Stop()

	// wait until 10 ticks have passed
	var count = 10
	var prevTick = time.Now()
	for i := 0; i < count; i++ {
		<-ticker.C
		prevTick = testTick(t, count, i, prevTick)
	}

	var elapsed = time.Since(startTime)
	require.LessOrEqual(t, elapsed.Microseconds(), profilerSamplingRate.Microseconds()*int64(count+3))
}

// This test measures the accuracy of time.Sleep() on the current system.
func TestProfilerTimeSleep(t *testing.T) {
	t.Skip("This test isn't necessary at the moment because we don't use time.Sleep() in the profiler.")

	onProfilerStart() // This fixes Windows ticker resolution.

	t.Logf("We're expecting a tick once every %d μs", profilerSamplingRate.Microseconds())

	var startTime = time.Now()

	// wait until 10 ticks have passed
	var count = 10
	var prevTick = time.Now()
	var next = time.Now()
	for i := 0; i < count; i++ {
		next = next.Add(profilerSamplingRate)
		time.Sleep(time.Until(next))
		prevTick = testTick(t, count, i, prevTick)
	}

	var elapsed = time.Since(startTime)
	require.LessOrEqual(t, elapsed.Microseconds(), profilerSamplingRate.Microseconds()*int64(count+3))
}

// Benchmark results (run without executing which mess up results)
// $ go test -run=^$ -bench "BenchmarkProfiler*"
//
// goos: windows
// goarch: amd64
// pkg: github.com/getsentry/sentry-go
// cpu: 12th Gen Intel(R) Core(TM) i7-12700K
// BenchmarkProfilerStartStop-20                      38008             31072 ns/op           20980 B/op        108 allocs/op
// BenchmarkProfilerOnTick-20                         65700             18065 ns/op             260 B/op          4 allocs/op
// BenchmarkProfilerCollect-20                        67063             16907 ns/op               0 B/op          0 allocs/op
// BenchmarkProfilerProcess-20                      2296788               512.9 ns/op           268 B/op          4 allocs/op
// BenchmarkProfilerOverheadBaseline-20                 192           6250525 ns/op
// BenchmarkProfilerOverheadWithProfiler-20             187           6249490 ns/op

func BenchmarkProfilerStartStop(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		stopFn := startProfiling(time.Now())
		_ = stopFn()
	}
}

func BenchmarkProfilerOnTick(b *testing.B) {
	profiler := newProfiler(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		profiler.onTick()
	}
}

func BenchmarkProfilerCollect(b *testing.B) {
	profiler := newProfiler(time.Now())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = profiler.collectRecords()
	}
}

func BenchmarkProfilerProcess(b *testing.B) {
	profiler := newProfiler(time.Now())
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
	stopFn := startProfiling(time.Now())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		doHardWork()
	}
	b.StopTimer()
	stopFn()
}
