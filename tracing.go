package sentry

import (
	"encoding/hex"
	"encoding/json"
	"time"
)

// A Span is the building block of a Sentry transaction. Spans build up a tree
// structure of timed operations. The span tree makes up a transaction event
// that is sent to Sentry when the root span is finished.
type Span struct {
	TraceID      TraceID                `json:"trace_id"`
	SpanID       SpanID                 `json:"span_id"`
	ParentSpanID SpanID                 `json:"parent_span_id"`
	Op           string                 `json:"op,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Status       SpanStatus             `json:"status,omitempty"`
	Tags         map[string]string      `json:"tags,omitempty"`
	StartTime    time.Time              `json:"start_timestamp"`
	EndTime      time.Time              `json:"timestamp"`
	Data         map[string]interface{} `json:"data,omitempty"`
}

// TODO: make Span.Tags and Span.Data opaque types (struct{unexported []slice}).
// An opaque type allows us to add methods and make it more convenient to use
// than maps, because maps require careful nil checks to use properly or rely on
// explicit initialization for every span, even when there might be no
// tags/data. For Span.Data, must gracefully handle values that cannot be
// marshaled into JSON (see transport.go:getRequestBodyFromEvent).

func (s *Span) MarshalJSON() ([]byte, error) {
	// span aliases Span to allow calling json.Marshal without an infinite loop.
	// It preserves all fields while none of the attached methods.
	type span Span
	var parentSpanID string
	if s.ParentSpanID != zeroSpanID {
		parentSpanID = s.ParentSpanID.String()
	}
	return json.Marshal(struct {
		*span
		ParentSpanID string `json:"parent_span_id,omitempty"`
	}{
		span:         (*span)(s),
		ParentSpanID: parentSpanID,
	})
}

// TraceID identifies a trace.
type TraceID [16]byte

func (id TraceID) Hex() []byte {
	b := make([]byte, hex.EncodedLen(len(id)))
	hex.Encode(b, id[:])
	return b
}

func (id TraceID) String() string {
	return string(id.Hex())
}

func (id TraceID) MarshalText() ([]byte, error) {
	return id.Hex(), nil
}

// SpanID identifies a span.
type SpanID [8]byte

func (id SpanID) Hex() []byte {
	b := make([]byte, hex.EncodedLen(len(id)))
	hex.Encode(b, id[:])
	return b
}

func (id SpanID) String() string {
	return string(id.Hex())
}

func (id SpanID) MarshalText() ([]byte, error) {
	return id.Hex(), nil
}

// Zero values of TraceID and SpanID used for comparisons.
var (
	//nolint // zeroTraceID TraceID
	zeroSpanID SpanID
)

// SpanStatus is the status of a span.
type SpanStatus uint8

// Implementation note:
//
// In Relay (ingestion), the SpanStatus type is an enum used as
// Annotated<SpanStatus> when embedded in structs, making it effectively
// Option<SpanStatus>. It means the status is either null or one of the known
// string values.
//
// In Snuba (search), the SpanStatus is stored as an uint8 and defaulted to 2
// ("unknown") when not set. It means that Discover searches for
// `transaction.status:unknown` return both transactions/spans with status
// `null` or `"unknown"`. Searches for `transaction.status:""` return nothing.
//
// With that in mind, the Go SDK default is SpanStatusUndefined, which is
// null/omitted when serializing to JSON, but integrations may update the status
// automatically based on contextual information.

const (
	SpanStatusUndefined SpanStatus = iota
	SpanStatusOK
	SpanStatusCanceled
	SpanStatusUnknown
	SpanStatusInvalidArgument
	SpanStatusDeadlineExceeded
	SpanStatusNotFound
	SpanStatusAlreadyExists
	SpanStatusPermissionDenied
	SpanStatusResourceExhausted
	SpanStatusFailedPrecondition
	SpanStatusAborted
	SpanStatusOutOfRange
	SpanStatusUnimplemented
	SpanStatusInternalError
	SpanStatusUnavailable
	SpanStatusDataLoss
	SpanStatusUnauthenticated
	maxSpanStatus
)

func (ss SpanStatus) String() string {
	if ss >= maxSpanStatus {
		return ""
	}
	m := [maxSpanStatus]string{
		"",
		"ok",
		"cancelled", // [sic]
		"unknown",
		"invalid_argument",
		"deadline_exceeded",
		"not_found",
		"already_exists",
		"permission_denied",
		"resource_exhausted",
		"failed_precondition",
		"aborted",
		"out_of_range",
		"unimplemented",
		"internal_error",
		"unavailable",
		"data_loss",
		"unauthenticated",
	}
	return m[ss]
}

func (ss SpanStatus) MarshalJSON() ([]byte, error) {
	s := ss.String()
	if s == "" {
		return []byte("null"), nil
	}
	return json.Marshal(s)
}

// A TraceContext carries information about an ongoing trace and is meant to be
// stored in Event.Contexts (as *TraceContext).
type TraceContext struct {
	TraceID      TraceID    `json:"trace_id"`
	SpanID       SpanID     `json:"span_id"`
	ParentSpanID SpanID     `json:"parent_span_id"`
	Op           string     `json:"op,omitempty"`
	Description  string     `json:"description,omitempty"`
	Status       SpanStatus `json:"status,omitempty"`
}

func (tc *TraceContext) MarshalJSON() ([]byte, error) {
	// traceContext aliases TraceContext to allow calling json.Marshal without
	// an infinite loop. It preserves all fields while none of the attached
	// methods.
	type traceContext TraceContext
	var parentSpanID string
	if tc.ParentSpanID != zeroSpanID {
		parentSpanID = tc.ParentSpanID.String()
	}
	return json.Marshal(struct {
		*traceContext
		ParentSpanID string `json:"parent_span_id,omitempty"`
	}{
		traceContext: (*traceContext)(tc),
		ParentSpanID: parentSpanID,
	})
}
