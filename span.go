package sentry

import (
	"time"
)

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
