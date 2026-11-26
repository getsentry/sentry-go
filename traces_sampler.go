package sentry

// A SamplingContext is passed to a TracesSampler to determine a sampling
// decision.
//
// TODO(tracing): possibly expand SamplingContext to include custom /
// user-provided data.
type SamplingContext struct {
	Span *Span // The current span, always non-nil.
	// ParentSampled is the sampling decision of the remote parent span.
	ParentSampled Sampled
	// ParentSampleRate is the sample rate used by the parent transaction.
	ParentSampleRate *float64
}

// The TracesSample type is an adapter to allow the use of ordinary
// functions as a TracesSampler.
type TracesSampler func(ctx SamplingContext) float64
