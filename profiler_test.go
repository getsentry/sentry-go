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
	trace := stopFn()
	validateProfile(t, trace, time.Since(start))
}

func doWorkFor(duration time.Duration) {
	start := time.Now()
	for time.Since(start) < duration {
		_ = findPrimeNumber(100)
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

	// Verify that sampling rate is correct works.
	var samplesByThread = map[uint64]int{}

	for _, sample := range trace.Samples {
		require.GreaterOrEqual(sample.ElapsedSinceStartNS, uint64(0))
		require.GreaterOrEqual(uint64(duration.Nanoseconds()), sample.ElapsedSinceStartNS)
		require.GreaterOrEqual(sample.StackID, 0)
		require.GreaterOrEqual(len(trace.Stacks), sample.StackID)
		require.Contains(trace.ThreadMetadata, strconv.Itoa(int(sample.ThreadID)))

		if prev, ok := samplesByThread[sample.ThreadID]; ok {
			samplesByThread[sample.ThreadID] = prev + 1
		} else {
			samplesByThread[sample.ThreadID] = 1
		}
	}

	expectedSampleCount := duration.Milliseconds() / profilerSamplingRate.Milliseconds()
	for threadID, numSamples := range samplesByThread {
		require.Equal(expectedSampleCount, numSamples, "goroutine %d sampling rate incorrect", threadID)
	}

	for _, thread := range trace.ThreadMetadata {
		require.NotEmpty(thread.Name)
	}

	for _, frame := range trace.Frames {
		require.NotEmpty(frame.Function)
		require.GreaterOrEqual(len(frame.AbsPath)+len(frame.Filename), 0)
		require.GreaterOrEqual(frame.Lineno, 0)
	}
}
