package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// A Span is the building block of a Sentry transaction. Spans build up a tree
// structure of timed operations. The span tree makes up a transaction event
// that is sent to Sentry when the root span is finished.
type Span interface {
	// Context returns the context containing the current span.
	Context() context.Context

	// SpanContext returns the data for the current span.
	SpanContext() SpanContext // or SpanContext for immutability, returning a copy
	// Set...(...) // to update SpanContext

	// Finish sets the current span's end time. If the current span is the root
	// of a span tree, Finish sends the span tree to Sentry as a transaction.
	Finish()

	// StartChild starts a new child span from the current span.
	//
	// The call current.StartChild(op, opts) is a shortcut equivalent to
	// StartSpan(current.Context(), op, opts).
	StartChild(operation string, options ...interface{ todo() }) Span

	// spanRecorder stores the span tree. It should never return nil.
	spanRecorder() *spanRecorder

	// A non-exported method prevents users from implementing the interface,
	// allowing it to grow later without breaking compatibility.
}

// spanContextKey is used to store span values in contexts.
type spanContextKey struct{}

// A RawSpan represents a span as it is sent over the network.
//
// Experimental: This is part of a beta feature of the SDK.
type RawSpan struct {
	TraceID      string                 `json:"trace_id"`
	SpanID       string                 `json:"span_id"`
	ParentSpanID string                 `json:"parent_span_id,omitempty"`
	Op           string                 `json:"op,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Status       string                 `json:"status,omitempty"`
	Tags         map[string]string      `json:"tags,omitempty"`
	StartTime    time.Time              `json:"start_timestamp"`
	EndTime      time.Time              `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

// A TraceContext represents part of the root span in a transaction event and is
// meant to be stored in Event.Contexts when sending transactions over the
// network.
//
// Experimental: This is part of a beta feature of the SDK.
type TraceContext struct {
	TraceID     string `json:"trace_id"`
	SpanID      string `json:"span_id"`
	Op          string `json:"op,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

// TraceID identifies a trace.
type TraceID [16]byte

// SpanID identifies a span.
type SpanID [8]byte

// SpanContext holds the information about a span necessary for trace
// propagation.
type SpanContext struct {
	TraceID TraceID
	SpanID  SpanID
	Sampled bool
}

// ToTraceparent returns the trace propagation value used with the sentry-trace
// HTTP header.
func (c SpanContext) ToTraceparent() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%x-%x-", c.TraceID, c.SpanID)
	if c.Sampled {
		b.WriteByte('1')
	} else {
		b.WriteByte('0')
	}
	return b.String()
}

// SpanFromContext returns the last span stored in the context or ........
//
// TODO: ensure this is really needed as public API ---
// 	SpanFromContext(ctx).StartChild(...) === StartSpan(ctx, ...)
// Do we need this for anything else?
// If we remove this we can also remove noopSpan.
func SpanFromContext(ctx context.Context) Span {
	if span, ok := ctx.Value(spanContextKey{}).(Span); ok {
		return span
	}
	return noopSpan{ctx: ctx}
}

// StartSpan starts a new span to describe an operation. The new span will be a
// child of the last span stored in ctx, if any.
func StartSpan(ctx context.Context, operation string, options ...interface{ todo() }) Span {
	var span normalSpan
	span.ctx = context.WithValue(ctx, spanContextKey{}, &span)

	parent, hasParent := ctx.Value(spanContextKey{}).(Span)
	if hasParent {
		span.parent = parent
		span.spanContext.TraceID = parent.SpanContext().TraceID
		span.recorder = parent.spanRecorder()
		if span.recorder == nil {
			panic("is nil")
		}
	} else {
		_, err := rand.Read(span.spanContext.TraceID[:]) // TODO: custom RNG
		if err != nil {
			panic(err)
		}
		span.recorder = &spanRecorder{}
	}
	span.recorder.spans = append(span.recorder.spans, &span)
	_, err := rand.Read(span.spanContext.SpanID[:]) // TODO: custom RNG
	if err != nil {
		panic(err)
	}

	// TODO: apply options

	// TODO: an option should be able to override the sampler, such that one can
	// disable sampling or force sampled:true for one specific transaction at
	// start time.
	hasSamplingDecision := false // Sampling Decision #1 (see https://develop.sentry.dev/sdk/unified-api/tracing/#sampling)

	// TODO: set StartTime

	if !hasSamplingDecision {
		hub := HubFromContext(ctx)
		var clientOptions ClientOptions
		client := hub.Client()
		if client != nil {
			clientOptions = hub.Client().Options() // TODO: check nil client
		}
		sampler := clientOptions.TracesSampler
		samplingContext := SamplingContext{Span: &span, Parent: parent}
		if sampler != nil {
			span.spanContext.Sampled = sampler.Sample(samplingContext) // Sampling Decision #2
		} else {
			if parent != nil {
				span.spanContext.Sampled = parent.SpanContext().Sampled // Sampling Decision #3
			} else {
				sampler = &fixedRateSampler{ // TODO: pre-compute the TracesSampler once and avoid extra computations in StartSpan.
					Rand: rand.New(rand.NewSource(1)), // TODO: use proper RNG
					Rate: clientOptions.TracesSampleRate,
				}
				span.spanContext.Sampled = sampler.Sample(samplingContext) // Sampling Decision #4
			}
		}
	}

	return &span
}

// WithTransactionName sets the transaction name of a span. Only the name of the
// root span in a span tree is used to name the transaction encompassing the
// tree.
func WithTransactionName(name string) interface{ todo() } {
	// TODO: to be implemented
	return nil
}

type noopSpan struct {
	ctx context.Context
}

var _ Span = noopSpan{}

func (s noopSpan) Context() context.Context { return s.ctx }
func (s noopSpan) SpanContext() SpanContext { return SpanContext{} }
func (s noopSpan) Finish()                  {}
func (s noopSpan) StartChild(operation string, options ...interface{ todo() }) Span {
	return StartSpan(s.ctx, operation, options...)
}
func (s noopSpan) spanRecorder() *spanRecorder { return &spanRecorder{} }

type normalSpan struct { // move to an internal package, rename to a cleaner name like Span
	ctx         context.Context
	spanContext SpanContext

	parent   Span
	recorder *spanRecorder
}

var _ Span = &normalSpan{}

func (s *normalSpan) Context() context.Context { return s.ctx }
func (s *normalSpan) SpanContext() SpanContext { return s.spanContext }
func (s *normalSpan) Finish()                  {}
func (s *normalSpan) StartChild(operation string, options ...interface{ todo() }) Span {
	return StartSpan(s.Context(), operation, options...)
}
func (s *normalSpan) spanRecorder() *spanRecorder { return s.recorder }

func (s *normalSpan) MarshalJSON() ([]byte, error) {
	return json.Marshal("hello")
}

// A spanRecorder stores a span tree that makes up a transaction.
type spanRecorder struct {
	spans []Span
}
