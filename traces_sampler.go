package sentry

import (
	"fmt"
)

// A TracesSampler makes sampling decisions for spans.
//
// In addition to the span passed to the Sample method, implementations may keep
// and use internal state to make decisions.
//
// Sampling is one of the last steps when starting a new span, such that the
// sampler can inspect most of the state of the span to make a decision.
type TracesSampler interface {
	Sample(ctx SamplingContext) bool
}

// The TracesSamplerFunc type is an adapter to allow the use of ordinary
// functions as a TracesSampler.
type TracesSamplerFunc func(ctx SamplingContext) bool

var _ TracesSampler = TracesSamplerFunc(nil)

func (f TracesSamplerFunc) Sample(ctx SamplingContext) bool {
	return f(ctx)
}

// A SamplingContext has data passed to a TracesSampler to determine a sampling
// decision.
type SamplingContext struct {
	Span   Span // The current span.
	Parent Span // The parent span, may be nil.
}

// fixedRateSampler is a TracesSampler that samples root spans randomly at a
// uniform rate. For non-root spans, it inherits the sampling decision from the
// parent span.
type fixedRateSampler struct { // TODO: make safe for concurrent use
	Rand interface {
		// Float64 returns a random number in [0.0, 1.0).
		Float64() float64
	}
	Rate float64
}

var _ TracesSampler = (*fixedRateSampler)(nil)

func (s *fixedRateSampler) Sample(ctx SamplingContext) bool {
	// Inherit the sampling decision if span is not the root of a span tree.
	if ctx.Parent != nil {
		return ctx.Parent.SpanContext().Sampled
	}
	if s.Rate < 0 || s.Rate > 1 {
		panic(fmt.Errorf("sampling rate out of range [0.0, 1.0]: %f", s.Rate))
	}
	return s.Rand.Float64() < s.Rate
}
