package sentry

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TraceIDFromHex(s string) TraceID {
	var id TraceID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func SpanIDFromHex(s string) SpanID {
	var id SpanID
	_, err := hex.Decode(id[:], []byte(s))
	if err != nil {
		panic(err)
	}
	return id
}

func TestSpanMarshalJSON(t *testing.T) {
	s := &Span{}
	testMarshalJSONOmitEmptyParentSpanID(t, s)
}

func TestSpanStatusMarshalJSON(t *testing.T) {
	tests := map[SpanStatus]string{
		SpanStatus(42):             `null`,
		SpanStatusUndefined:        `null`,
		SpanStatusOK:               `"ok"`,
		SpanStatusDeadlineExceeded: `"deadline_exceeded"`,
		SpanStatusCanceled:         `"cancelled"`,
	}
	for s, want := range tests {
		s, want := s, want
		t.Run(fmt.Sprintf("SpanStatus(%d)", s), func(t *testing.T) {
			b, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			got := string(b)
			if got != want {
				t.Fatalf("got %s, want %s", got, want)
			}
		})
	}
}

func TestTraceContextMarshalJSON(t *testing.T) {
	tc := &TraceContext{}
	testMarshalJSONOmitEmptyParentSpanID(t, tc)
}

func testMarshalJSONOmitEmptyParentSpanID(t *testing.T, v interface{}) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte("parent_span_id")) {
		t.Fatalf("unwanted parent_span_id: %s", b)
	}
	id := reflect.ValueOf(SpanIDFromHex("c7b73e77a3734fee"))
	reflect.ValueOf(v).Elem().FieldByName("ParentSpanID").Set(id)
	b, err = json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("parent_span_id")) {
		t.Fatalf("missing parent_span_id: %s", b)
	}
}

func TestStartSpan(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		Transport: transport,
	})
	op := "test.op"
	transaction := "Test Transaction"
	description := "A Description"
	status := SpanStatusOK
	parentSpanID := SpanIDFromHex("f00db33f")
	sampled := SampledTrue
	startTime := time.Now()
	endTime := startTime.Add(3 * time.Second)
	data := map[string]interface{}{
		"k": "v",
	}
	span := StartSpan(ctx, op,
		TransactionName(transaction),
		func(s *Span) {
			s.Description = description
			s.Status = status
			s.ParentSpanID = parentSpanID
			s.Sampled = sampled
			s.StartTime = startTime
			s.EndTime = endTime
			s.Data = data
		},
	)
	span.Finish()

	SpanCheck{
		Sampled:     sampled,
		RecorderLen: 1,
	}.Check(t, span)

	events := transport.Events()
	if got := len(events); got != 1 {
		t.Fatalf("sent %d events, want 1", got)
	}
	want := &Event{
		Type:        transactionType,
		Transaction: transaction,
		Contexts: map[string]interface{}{
			"trace": &TraceContext{
				TraceID:      span.TraceID,
				SpanID:       span.SpanID,
				ParentSpanID: parentSpanID,
				Op:           op,
				Description:  description,
				Status:       status,
			},
		},
		Tags: nil,
		// TODO(tracing): the root span / transaction data field is
		// mapped into Event.Extra for now, pending spec clarification.
		// https://github.com/getsentry/develop/issues/244#issuecomment-778694182
		Extra:     span.Data,
		Timestamp: endTime,
		StartTime: startTime,
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{},
			"Contexts", "EventID", "Level", "Platform",
			"Release", "Sdk", "ServerName",
		),
		cmpopts.EquateEmpty(),
	}
	if diff := cmp.Diff(want, events[0], opts); diff != "" {
		t.Fatalf("Event mismatch (-want +got):\n%s", diff)
	}
	// Check trace context explicitly, as we ignored all contexts above to
	// disregard other contexts.
	if diff := cmp.Diff(want.Contexts["trace"], events[0].Contexts["trace"]); diff != "" {
		t.Fatalf("TraceContext mismatch (-want +got):\n%s", diff)
	}
}

func TestStartChild(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		TracesSampleRate: 1.0,
		Transport:        transport,
	})
	span := StartSpan(ctx, "top", TransactionName("Test Transaction"))
	child := span.StartChild("child")
	child.Finish()
	span.Finish()

	c := SpanCheck{
		Sampled:     SampledTrue,
		RecorderLen: 2,
	}
	c.Check(t, span)
	c.Check(t, child)

	events := transport.Events()
	if got := len(events); got != 1 {
		t.Fatalf("sent %d events, want 1", got)
	}
	want := &Event{
		Type:        transactionType,
		Transaction: "Test Transaction",
		Contexts: map[string]interface{}{
			"trace": &TraceContext{
				TraceID: span.TraceID,
				SpanID:  span.SpanID,
				Op:      span.Op,
			},
		},
		Spans: []*Span{
			{
				TraceID:      child.TraceID,
				SpanID:       child.SpanID,
				ParentSpanID: child.ParentSpanID,
				Op:           child.Op,
				Sampled:      SampledTrue,
			},
		},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{},
			"EventID", "Level", "Platform",
			"Release", "Sdk", "ServerName", "Timestamp", "StartTime",
		),
		cmpopts.IgnoreMapEntries(func(k string, v interface{}) bool {
			return k != "trace"
		}),
		cmpopts.IgnoreFields(Span{},
			"StartTime", "EndTime",
		),
		cmpopts.IgnoreUnexported(Span{}),
		cmpopts.EquateEmpty(),
	}
	if diff := cmp.Diff(want, events[0], opts); diff != "" {
		t.Fatalf("Event mismatch (-want +got):\n%s", diff)
	}
}

// testContextKey is used to store a value in a context so that we can check
// that SDK operations on that context preserve the original context values.
type testContextKey struct{}
type testContextValue struct{}

func NewTestContext(options ClientOptions) context.Context {
	client, err := NewClient(options)
	if err != nil {
		panic(err)
	}
	hub := NewHub(client, NewScope())
	ctx := context.WithValue(context.Background(), testContextKey{}, testContextValue{})
	return SetHubOnContext(ctx, hub)
}

// A SpanCheck is a test helper describing span properties that can be checked
// with the Check method.
type SpanCheck struct {
	Sampled     Sampled
	ZeroTraceID bool
	ZeroSpanID  bool
	RecorderLen int
}

func (c SpanCheck) Check(t *testing.T, span *Span) {
	t.Helper()

	// Invariant: original context values are preserved
	gotCtx := span.Context()
	if _, ok := gotCtx.Value(testContextKey{}).(testContextValue); !ok {
		t.Errorf("original context value lost")
	}
	// Invariant: SpanFromContext(span.Context) == span
	if spanFromContext(gotCtx) != span {
		t.Errorf("span not in its context")
	}

	if got := span.TraceID == zeroTraceID; got != c.ZeroTraceID {
		want := "zero"
		if !c.ZeroTraceID {
			want = "non-" + want
		}
		t.Errorf("got TraceID = %s, want %s", span.TraceID, want)
	}
	if got := span.SpanID == zeroSpanID; got != c.ZeroSpanID {
		want := "zero"
		if !c.ZeroSpanID {
			want = "non-" + want
		}
		t.Errorf("got SpanID = %s, want %s", span.SpanID, want)
	}
	if got, want := span.Sampled, c.Sampled; got != want {
		t.Errorf("got Sampled = %v, want %v", got, want)
	}

	if got, want := len(span.spanRecorder().spans), c.RecorderLen; got != want {
		t.Errorf("got %d spans in recorder, want %d", got, want)
	}

	if span.StartTime.IsZero() {
		t.Error("start time not set")
	}
	if span.EndTime.IsZero() {
		t.Error("end time not set")
	}
	if span.EndTime.Before(span.StartTime) {
		t.Error("end time before start time")
	}
}

func TestToSentryTrace(t *testing.T) {
	tests := []struct {
		span *Span
		want string
	}{
		{&Span{}, "00000000000000000000000000000000-0000000000000000"},
		{&Span{Sampled: SampledTrue}, "00000000000000000000000000000000-0000000000000000-1"},
		{&Span{Sampled: SampledFalse}, "00000000000000000000000000000000-0000000000000000-0"},
		{&Span{TraceID: TraceID{1}}, "01000000000000000000000000000000-0000000000000000"},
		{&Span{SpanID: SpanID{1}}, "00000000000000000000000000000000-0100000000000000"},
	}
	for _, tt := range tests {
		if got := tt.span.ToSentryTrace(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}

func TestContinueSpanFromRequest(t *testing.T) {
	traceID := TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4")
	spanID := SpanIDFromHex("b72fa28504b07285")

	for _, sampled := range []Sampled{SampledTrue, SampledFalse, SampledUndefined} {
		sampled := sampled
		t.Run(sampled.String(), func(t *testing.T) {
			var s Span
			hkey := http.CanonicalHeaderKey("sentry-trace")
			hval := (&Span{
				TraceID: traceID,
				SpanID:  spanID,
				Sampled: sampled,
			}).ToSentryTrace()
			header := http.Header{hkey: []string{hval}}
			ContinueFromRequest(&http.Request{Header: header})(&s)
			if s.TraceID != traceID {
				t.Errorf("got %q, want %q", s.TraceID, traceID)
			}
			if s.ParentSpanID != spanID {
				t.Errorf("got %q, want %q", s.ParentSpanID, spanID)
			}
			if s.Sampled != sampled {
				t.Errorf("got %q, want %q", s.Sampled, sampled)
			}
		})
	}
}

func TestSpanFromContext(t *testing.T) {
	// SpanFromContext always returns a non-nil value, such that you can use
	// it without nil checks.
	// When no span was in the context, the returned value is a no-op.
	// Calling StartChild on the no-op creates a valid transaction.
	// SpanFromContext(ctx).StartChild(...) === StartSpan(ctx, ...)

	ctx := NewTestContext(ClientOptions{})
	span := spanFromContext(ctx)

	_ = span

	// SpanCheck{
	// 	ZeroTraceID: true,
	// 	ZeroSpanID:  true,
	// }.Check(t, span)

	// // Should create a transaction
	// child := span.StartChild("top")
	// SpanCheck{
	// 	RecorderLen: 1,
	// }.Check(t, child)
}

func TestDoubleSampling(t *testing.T) {
	transport := &TransportMock{}
	ctx := NewTestContext(ClientOptions{
		SampleRate:       math.SmallestNonzeroFloat64,
		TracesSampleRate: 1.0,
		Transport:        transport,
	})
	span := StartSpan(ctx, "op", TransactionName("name"))

	// CaptureException should not send any event because of SampleRate.
	GetHubFromContext(ctx).CaptureException(errors.New("ignored"))
	if got := len(transport.Events()); got != 0 {
		t.Fatalf("got %d events, want 0", got)
	}

	// Finish should send one transaction event, always sampled via
	// TracesSampleRate.
	span.Finish()
	if got := len(transport.Events()); got != 1 {
		t.Fatalf("got %d events, want 1", got)
	}
	if got := transport.Events()[0].Type; got != transactionType {
		t.Fatalf("got %v event, want %v", got, transactionType)
	}
}
