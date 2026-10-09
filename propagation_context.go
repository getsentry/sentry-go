package sentry

import (
	"crypto/rand"
	"maps"
)

const (
	traceIDContextKey = "trace_id"
	spanIDContextKey  = "span_id"
)

type PropagationContext struct {
	TraceID                TraceID                `json:"trace_id"`
	SpanID                 SpanID                 `json:"span_id"`
	ParentSpanID           SpanID                 `json:"parent_span_id,omitzero"`
	DynamicSamplingContext DynamicSamplingContext `json:"-"`
}

func (p PropagationContext) clone() PropagationContext {
	p.DynamicSamplingContext.Entries = maps.Clone(p.DynamicSamplingContext.Entries)
	return p
}

func (p PropagationContext) Map() map[string]interface{} {
	m := map[string]interface{}{
		traceIDContextKey: p.TraceID,
		spanIDContextKey:  p.SpanID,
	}

	if p.ParentSpanID != zeroSpanID {
		m["parent_span_id"] = p.ParentSpanID
	}

	return m
}

func NewPropagationContext() PropagationContext {
	p := PropagationContext{}

	if _, err := rand.Read(p.TraceID[:]); err != nil {
		panic(err)
	}

	if _, err := rand.Read(p.SpanID[:]); err != nil {
		panic(err)
	}

	return p
}

func PropagationContextFromHeaders(trace, baggage string) (PropagationContext, error) {
	p := NewPropagationContext()

	if _, err := rand.Read(p.SpanID[:]); err != nil {
		panic(err)
	}

	hasTrace := false
	if trace != "" {
		if tpc, valid := ParseTraceParentContext([]byte(trace)); valid {
			hasTrace = true
			p.TraceID = tpc.TraceID
			p.ParentSpanID = tpc.ParentSpanID
		}
	}

	if baggage != "" {
		dsc, err := DynamicSamplingContextFromHeader([]byte(baggage))
		if err != nil {
			return PropagationContext{}, err
		}
		p.DynamicSamplingContext = dsc
	}

	// In case a sentry-trace header is present but there are no sentry-related
	// values in the baggage, create an empty, frozen DynamicSamplingContext.
	if hasTrace && !p.DynamicSamplingContext.HasEntries() {
		p.DynamicSamplingContext = DynamicSamplingContext{
			Frozen: true,
		}
	}

	return p, nil
}
