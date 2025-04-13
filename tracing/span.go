package sentrytracing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"sync"
	"time"
)

type TraceID [16]byte
type SpanID [8]byte

type SpanStatus string

type currentSpanKey int

const currentSpan currentSpanKey = iota

const (
	SpanStatusOK    SpanStatus = "ok"
	SpanStatusError SpanStatus = "error"
)

type Span struct {
	TraceID      TraceID    `json:"trace_id"`
	SpanID       SpanID     `json:"span_id"`
	ParentSpanID SpanID     `json:"parent_span_id"`
	Name         string     `json:"name,omitempty"`
	Status       SpanStatus `json:"status,omitempty"`
	StartTime    time.Time  `json:"start_timestamp"`
	EndTime      time.Time  `json:"timestamp"`
	Attributes   []any

	mu         sync.Mutex
	finishOnce sync.Once
	ctx        context.Context
}

func StartSpan(ctx context.Context, name string) (context.Context, *Span) {
	parent, hasParent := ctx.Value(currentSpan).(*Span)

	span := &Span{
		StartTime: time.Now(),
		Name:      name,
	}

	if hasParent {
		span.TraceID = parent.TraceID
		span.ParentSpanID = parent.SpanID
	} else {
		_, err := rand.Read(span.TraceID[:])
		if err != nil {
			panic(err)
		}
		_, err = rand.Read(span.SpanID[:])
		if err != nil {
			panic(err)
		}
	}

	span.ctx = context.WithValue(ctx, currentSpan, span)
	return ctx, span
}

func (s *Span) Finish() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.finishOnce.Do(func() {
		s.EndTime = s.StartTime.Add(time.Since(s.StartTime))
	})
}

func (s *Span) SetAttributes(attributes ...AttributeBuilder) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Attributes = make([]any, len(attributes))
	for i, attr := range attributes {
		if builder, ok := attr.(*attributeBuilder); ok {
			s.Attributes[i] = builder.attr
		}
	}
}

func (s *Span) MarshalJSON() ([]byte, error) {
	return json.Marshal(s)
}
