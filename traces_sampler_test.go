package sentry

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestFixedRateSampler(t *testing.T) {
	ctx := NewTestContext(ClientOptions{})
	rootSpan := StartSpan(ctx, "root").(*normalSpan)
	childSpan := rootSpan.StartChild("child")

	s := &fixedRateSampler{
		Rand: rand.New(rand.NewSource(1)),
	}

	t.Run("Inheritance", func(t *testing.T) {
		// The sample decision for a child span should inherit its parent decision.
		for want, parentDecision := range [...]bool{false, true} {
			rootSpan.spanContext.Sampled = parentDecision
			for _, rate := range []float64{0.0, 0.25, 0.5, 0.75, 1.0} {
				s.Rate = rate
				if got := repeatedSample(s, SamplingContext{Span: childSpan, Parent: rootSpan}, 10000); got != float64(want) {
					t.Errorf("got childSpan sample rate %.2f, want %d", got, want)
				}
			}
		}
	})

	t.Run("UniformRate", func(t *testing.T) {
		// The sample decision for the root span should observe the configured
		// rate.
		tests := []struct {
			Rate      float64
			Tolerance float64
		}{
			{0.0, 0.0},
			{0.25, 0.1},
			{0.5, 0.1},
			{0.75, 0.1},
			{1.0, 0.0},
		}
		for _, tt := range tests {
			tt := tt
			t.Run(fmt.Sprint(tt.Rate), func(t *testing.T) {
				s.Rate = tt.Rate
				if got := repeatedSample(s, SamplingContext{Span: rootSpan}, 10000); got < tt.Rate*(1-tt.Tolerance) || got > tt.Rate*(1+tt.Tolerance) {
					t.Errorf("got rootSpan sample rate %.2f, want %.2f±%.0f%%", got, tt.Rate, 100*tt.Tolerance)
				}
			})
		}
	})

	// t.Run("Concurrency", func(t *testing.T) {
	// 	var wg sync.WaitGroup
	// 	s.Rate = 0.5
	// 	for i := 0; i < 32; i++ {
	// 		wg.Add(1)
	// 		go func() {
	// 			defer wg.Done()
	// 			repeatedSample(s, SamplingContext{Span: rootSpan}, 10000)
	// 		}()
	// 	}
	// })
}

func repeatedSample(sampler TracesSampler, ctx SamplingContext, count int) (observedRate float64) {
	var n float64
	for i := 0; i < count; i++ {
		if sampler.Sample(ctx) {
			n++
		}
	}
	return n / float64(count)
}
