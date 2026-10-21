package sentry

import (
	"crypto/rand"
	"maps"
)

const (
	traceContextKey   = "trace"
	traceIDContextKey = "trace_id"
	spanIDContextKey  = "span_id"
)

type PropagationContext struct {
	TraceID                TraceID                `json:"trace_id"`
	SpanID                 SpanID                 `json:"span_id"`
	ParentSpanID           SpanID                 `json:"parent_span_id,omitzero"`
	Sampled                Sampled                `json:"-"`
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

// PropagationContextFromHeaders extracts incoming trace and sampling metadata.
// Malformed baggage returns an error together with a usable propagation context;
// a valid sentry-trace header is retained with empty, frozen sampling metadata.
func PropagationContextFromHeaders(trace, baggage string) (PropagationContext, error) {
	p := NewPropagationContext()
	parsed, dsc, valid, err := parseIncomingTrace(trace, baggage)
	if valid {
		p.TraceID = parsed.TraceID
		p.ParentSpanID = parsed.ParentSpanID
		p.Sampled = parsed.Sampled
		if dscMatchesTrace(parsed, dsc) {
			p.DynamicSamplingContext = dsc
		} else {
			p.DynamicSamplingContext = DynamicSamplingContext{Frozen: true}
		}
	}
	return p, err
}
