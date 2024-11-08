package sentryotel

// Context keys to be used with context.WithValue(...) and ctx.Value(...)
type dynamicSamplingContextKey struct{}
type sentryTraceHeaderContextKey struct{}
type sentryTraceParentContextKey struct{}
type baggageContextKey struct{}
