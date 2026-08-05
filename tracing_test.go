package sentry

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
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
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
		Transport:     transport,
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
		WithTransactionName(transaction),
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
		Contexts: map[string]Context{
			"trace": TraceContext{
				TraceID:      span.TraceID,
				SpanID:       span.SpanID,
				ParentSpanID: parentSpanID,
				Op:           op,
				Data:         span.Data,
				Description:  description,
				Status:       status,
			}.Map(),
		},
		Tags:      nil,
		Timestamp: endTime,
		StartTime: startTime,
		TransactionInfo: &TransactionInfo{
			Source: span.Source,
		},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{},
			"Contexts", "EventID", "Level", "Platform",
			"Release", "Sdk", "ServerName", "Modules",
		),
		cmpopts.IgnoreFields(Event{}, "sdkMetaData", "serializedTags", "serializedContexts", "serializedBreadcrumbs", "serializedException", "serializedUser", "serializationSafe"),
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
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
	})
	span := StartSpan(ctx, "top", WithTransactionName("Test Transaction"))
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
		Contexts: map[string]Context{
			"trace": TraceContext{
				TraceID: span.TraceID,
				SpanID:  span.SpanID,
				Op:      span.Op,
			}.Map(),
		},
		Spans: []*Span{
			{
				TraceID:      child.TraceID,
				SpanID:       child.SpanID,
				ParentSpanID: child.ParentSpanID,
				Op:           child.Op,
				Sampled:      SampledTrue,
				Origin:       SpanOriginManual,
			},
		},
		TransactionInfo: &TransactionInfo{
			Source: span.Source,
		},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{},
			"EventID", "Level", "Platform", "Modules",
			"Release", "Sdk", "ServerName", "Timestamp", "StartTime",
		),
		cmpopts.IgnoreFields(Event{}, "sdkMetaData", "serializedTags", "serializedContexts", "serializedBreadcrumbs", "serializedException", "serializedUser", "serializationSafe"),
		cmpopts.IgnoreMapEntries(func(k string, _ interface{}) bool {
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

func TestStartTransaction(t *testing.T) {
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
		Transport:     transport,
	})
	transactionName := "Test Transaction"
	description := "A Description"
	status := SpanStatusOK
	sampled := SampledTrue
	startTime := time.Now()
	endTime := startTime.Add(3 * time.Second)
	data := map[string]interface{}{
		"k": "v",
	}
	transaction := StartTransaction(ctx,
		transactionName,
		func(s *Span) {
			s.Description = description
			s.Status = status
			s.Sampled = sampled
			s.StartTime = startTime
			s.EndTime = endTime
			s.Data = data
			s.SetContext("otel", Context{"k": "v"})
		},
	)
	transaction.Finish()

	SpanCheck{
		Sampled:     sampled,
		RecorderLen: 1,
	}.Check(t, transaction)

	events := transport.Events()
	if got := len(events); got != 1 {
		t.Fatalf("sent %d events, want 1", got)
	}
	want := &Event{
		Type:        transactionType,
		Transaction: transactionName,
		Contexts: map[string]Context{
			"trace": TraceContext{
				TraceID:     transaction.TraceID,
				SpanID:      transaction.SpanID,
				Data:        transaction.Data,
				Description: description,
				Status:      status,
			}.Map(),
			"otel": {"k": "v"},
		},
		Tags:      nil,
		Timestamp: endTime,
		StartTime: startTime,
		TransactionInfo: &TransactionInfo{
			Source: transaction.Source,
		},
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(Event{},
			"Contexts", "EventID", "Level", "Platform",
			"Release", "Sdk", "ServerName", "Modules",
		),
		cmpopts.IgnoreFields(Event{}, "sdkMetaData", "serializedTags", "serializedContexts", "serializedBreadcrumbs", "serializedException", "serializedUser", "serializationSafe"),
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

func TestStartTransaction_with_context(t *testing.T) {
	t.Run("get transaction from context", func(t *testing.T) {
		tr := StartTransaction(context.TODO(), "")
		ctx := tr.Context()
		existingTr := StartTransaction(ctx, "")
		if existingTr != tr {
			t.Fatalf("existing transaction not found")
		}
	})

	t.Run("get transaction with latest context", func(t *testing.T) {
		tr := StartTransaction(context.TODO(), "")
		ctx := context.WithValue(tr.Context(), testContextKey{}, testContextValue{})
		existingTr := StartTransaction(ctx, "")
		_, keyExists := existingTr.Context().Value(testContextKey{}).(testContextValue)
		if !keyExists {
			t.Fatalf("key not found in context")
		}
	})
}

func TestSetTag(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	span := StartSpan(ctx, "Test Span")
	span.SetTag("key", "value")

	if (span.Tags == nil) || (span.Tags["key"] != "value") {
		t.Fatalf("Tags mismatch, got %v", span.Tags)
	}
}

func TestSetData(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	span := StartSpan(ctx, "Test Span")
	span.SetData("key", "value")
	span.SetData("key.nil", nil)
	span.SetData("key.number", 123)
	span.SetData("key.bool", true)
	span.SetData("key.slice", []string{"foo", "bar"})
	if (span.Data == nil) || (span.Data["key"] != "value") || (span.Data["key.number"] != 123) || (span.Data["key.bool"] != true) || !reflect.DeepEqual(span.Data["key.slice"], []string{"foo", "bar"}) {
		t.Fatalf("Data mismatch, got %v", span.Data)
	}
}

func TestWithDescription(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	span := StartSpan(ctx, "Test Span", WithDescription("span desc"))
	if span.Description != "span desc" {
		t.Fatalf("Description mismatch, got %v", span.Description)
	}
}

func TestIsTransaction(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})

	transaction := StartTransaction(ctx, "Test Transaction")
	if !transaction.IsTransaction() {
		t.Fatalf("span.IsTransaction() = false, want true")
	}

	span := transaction.StartChild("Test Span")
	if span.IsTransaction() {
		t.Fatalf("span.IsTransaction() = true, want false")
	}
}

// testContextKey is used to store a value in a context so that we can check
// that SDK operations on that context preserve the original context values.
type testContextKey struct{}
type testContextValue struct{}

func NewTestContext(options ClientOptions) context.Context {
	if options.Transport == nil {
		options.Transport = &MockTransport{}
	}
	client, err := newClient(options)
	if err != nil {
		panic(err)
	}
	hub := NewHub(client, NewScope())
	ctx := context.WithValue(context.Background(), testContextKey{}, testContextValue{})
	ctx = SetHubOnContext(ctx, hub)
	ctx, scope := ScopeFromContext(ctx)
	scope.SetClient(client)
	return ctx
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
	if SpanFromContext(gotCtx) != span {
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
func TestSpanFromContext(_ *testing.T) {
	// SpanFromContext always returns a non-nil value, such that you can use
	// it without nil checks.
	// When no span was in the context, the returned value is a no-op.
	// Calling StartChild on the no-op creates a valid transaction.
	// SpanFromContext(ctx).StartChild(...) === StartSpan(ctx, ...)

	ctx := NewTestContext(ClientOptions{})
	span := SpanFromContext(ctx)

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
	transport := &MockTransport{}
	ctx := NewTestContext(ClientOptions{
		// A SampleRate set to 0.0 will be transformed to 1.0,
		// hence we're using math.SmallestNonzeroFloat64.
		SampleRate:       math.SmallestNonzeroFloat64,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
	})
	span := StartSpan(ctx, "op", WithTransactionName("name"))

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

func TestSample(t *testing.T) {
	var ctx context.Context
	var span *Span

	// tracing is disabled
	ctx = NewTestContext(ClientOptions{
		EnableTracing: false,
	})
	span = StartSpan(ctx, "op", WithTransactionName("name"))
	if got := span.Sampled; got != SampledFalse {
		t.Fatalf("got %s, want %s", got, SampledFalse)
	}

	// explicit sampling decision
	ctx = NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 0.0,
	})
	span = StartSpan(ctx, "op", WithTransactionName("name"), WithSpanSampled(SampledTrue))
	if got := span.explicitSampled; got != SampledTrue {
		t.Fatalf("got %s, want %s", got, SampledTrue)
	}

	// traces sampler
	ctx = NewTestContext(ClientOptions{
		EnableTracing: true,
		TracesSampler: func(_ SamplingContext) float64 {
			return 1.0
		},
	})
	span = StartSpan(ctx, "op", WithTransactionName("name"))
	if got := span.Sampled; got != SampledTrue {
		t.Fatalf("got %s, want %s", got, SampledTrue)
	}

	// parent sampling decision
	ctx = NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	span = StartSpan(ctx, "op", WithTransactionName("name"))
	childSpan := span.StartChild("child")
	if got := childSpan.Sampled; got != SampledTrue {
		t.Fatalf("got %s, want %s", got, SampledTrue)
	}

	// traces sample rate
	ctx = NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	span = StartSpan(ctx, "op", WithTransactionName("name"))
	if got := span.Sampled; got != SampledTrue {
		t.Fatalf("got %s, want %s", got, SampledTrue)
	}
}

func TestSampleRatePropagation(t *testing.T) {
	tests := []struct {
		name                   string
		clientOptions          ClientOptions
		traceHeader            string
		baggageHeader          string
		expectedRate           float64
		expectedBaggageEntries []string
	}{
		{
			name: "Tracing disabled",
			clientOptions: ClientOptions{
				EnableTracing: false,
			},
			traceHeader:            "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-1",
			baggageHeader:          "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=true,sentry-sample_rate=1",
			expectedRate:           0.0,
			expectedBaggageEntries: nil,
		},
		{
			name: "Inherit from parent - sampled flag = 1",
			clientOptions: ClientOptions{
				EnableTracing: true,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-1",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=true,sentry-sample_rate=1",
			expectedRate:  1.0,
			expectedBaggageEntries: []string{
				"sentry-sampled=true",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=1",
			},
		},
		{
			name: "Inherit from parent - sampled flag = 0",
			clientOptions: ClientOptions{
				EnableTracing: true,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-0",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=false,sentry-sample_rate=0.0",
			expectedRate:  0.0,
			expectedBaggageEntries: []string{
				"sentry-sampled=false",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0",
			},
		},
		{
			name: "Inherit from parent - defer sampled flag",
			clientOptions: ClientOptions{
				EnableTracing: true,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
			expectedRate:  0.0,
			expectedBaggageEntries: []string{
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0",
			},
		},
		{
			name: "TracesSampler with sampled flag = 1",
			clientOptions: ClientOptions{
				EnableTracing: true,
				TracesSampler: func(_ SamplingContext) float64 {
					return 0.8
				},
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-1",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=true,sentry-sample_rate=1",
			expectedRate:  0.8,
			expectedBaggageEntries: []string{
				"sentry-sampled=true",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0.8",
			},
		},
		{
			name: "TracesSampler with sampled flag = 0",
			clientOptions: ClientOptions{
				EnableTracing: true,
				TracesSampler: func(_ SamplingContext) float64 {
					return 0.8
				},
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-0",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=false,sentry-sample_rate=0.0",
			expectedRate:  0.8,
			expectedBaggageEntries: []string{
				"sentry-sampled=false",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0.8",
			},
		},
		{
			name: "TracesSampler - defer sampled flag",
			clientOptions: ClientOptions{
				EnableTracing: true,
				TracesSampler: func(_ SamplingContext) float64 {
					return 0.8
				},
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
			expectedRate:  0.8,
			expectedBaggageEntries: []string{
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0.8",
			},
		},
		{
			name: "TracesSampleRate with sampled flag = 1",
			clientOptions: ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 0.4,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-1",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=true,sentry-sample_rate=1",
			expectedRate:  1.0,
			expectedBaggageEntries: []string{
				"sentry-sampled=true",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=1",
			},
		},
		{
			name: "TracesSampleRate with sampled flag = 0",
			clientOptions: ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 0.4,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-0",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=false,sentry-sample_rate=0.0",
			expectedRate:  0.0,
			expectedBaggageEntries: []string{
				"sentry-sampled=false",
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0",
			},
		},
		{
			name: "TracesSampleRate - defer sampled flag",
			clientOptions: ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 0.4,
			},
			traceHeader:   "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963",
			baggageHeader: "sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
			expectedRate:  0.4,
			expectedBaggageEntries: []string{
				"sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
				"sentry-sample_rate=0.4",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &MockTransport{}
			ctx := NewTestContext(ClientOptions{
				EnableTracing:    tt.clientOptions.EnableTracing,
				TracesSampler:    tt.clientOptions.TracesSampler,
				TracesSampleRate: tt.clientOptions.TracesSampleRate,
				Transport:        transport,
			})

			ctx = ContinueTrace(ctx, tt.traceHeader, tt.baggageHeader)
			transaction := StartTransaction(ctx, "test-transaction")
			transaction.Finish()

			baggage := transaction.ToBaggage()
			for _, header := range tt.expectedBaggageEntries {
				if !strings.Contains(baggage, header) {
					t.Errorf("Expected baggage header to contain %q, got %q", header, baggage)
				}
			}

			if transaction.sampleRate != tt.expectedRate {
				t.Errorf("Expected sample rate %f, got %f", tt.expectedRate, transaction.sampleRate)
			}
		})
	}
}

func TestTracesSamplerReceivesRemoteParent(t *testing.T) {
	t.Parallel()

	ptrFloat := func(f float64) *float64 { return &f }

	tests := []struct {
		name                 string
		traceHeader          string
		baggageHeader        string
		wantParentSampled    Sampled
		wantParentSampleRate *float64
	}{
		{
			name:                 "remote parent sampled=true with rate",
			traceHeader:          "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-1",
			baggageHeader:        "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=true,sentry-sample_rate=0.8",
			wantParentSampled:    SampledTrue,
			wantParentSampleRate: ptrFloat(0.8),
		},
		{
			name:                 "remote parent sampled=false with rate",
			traceHeader:          "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963-0",
			baggageHeader:        "sentry-trace_id=423d7a0fb16128c8503f067d8447caba,sentry-sampled=false,sentry-sample_rate=0.1",
			wantParentSampled:    SampledFalse,
			wantParentSampleRate: ptrFloat(0.1),
		},
		{
			name:                 "remote parent sampled=defer no rate",
			traceHeader:          "423d7a0fb16128c8503f067d8447caba-d9246d56c61fc963",
			baggageHeader:        "sentry-trace_id=423d7a0fb16128c8503f067d8447caba",
			wantParentSampled:    SampledUndefined,
			wantParentSampleRate: nil,
		},
		{
			name:                 "no remote parent",
			traceHeader:          "",
			baggageHeader:        "",
			wantParentSampled:    SampledUndefined,
			wantParentSampleRate: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotCtx SamplingContext
			ctx := NewTestContext(ClientOptions{
				EnableTracing: true,
				TracesSampler: func(samplingContext SamplingContext) float64 {
					gotCtx = samplingContext
					return 1.0
				},
			})

			ctx = ContinueTrace(ctx, tt.traceHeader, tt.baggageHeader)
			txn := StartTransaction(ctx, "test-txn")
			txn.Finish()

			assert.Nil(t, gotCtx.Parent, "SamplingContext.Parent should be nil for remote parent")
			assert.Equal(t, tt.wantParentSampled, gotCtx.ParentSampled)
			assert.Equal(t, tt.wantParentSampleRate, gotCtx.ParentSampleRate)
		})
	}
}

func TestDoesNotCrashWithEmptyContext(_ *testing.T) {
	// This test makes sure that we can still start and finish transactions
	// with empty context (for example, when Sentry SDK is not initialized)
	ctx := context.Background()
	tx := StartTransaction(ctx, "op")
	tx.Sampled = SampledTrue
	tx.Finish()
}

func TestSetDynamicSamplingContextWorksOnTransaction(t *testing.T) {
	s := Span{
		dynamicSamplingContext: DynamicSamplingContext{Frozen: false},
	}

	newDsc := DynamicSamplingContext{
		Entries: map[string]string{"environment": "dev"},
		Frozen:  true,
	}

	s.SetDynamicSamplingContext(newDsc)

	if diff := cmp.Diff(newDsc, s.dynamicSamplingContext); diff != "" {
		t.Errorf("DynamicSamplingContext mismatch (-want +got):\n%s", diff)
	}
}

func TestSetDynamicSamplingContextDoesNothingOnSpan(t *testing.T) {
	// SetDynamicSamplingContext should do nothing on non-transaction spans
	s := Span{
		parent:                 &Span{},
		dynamicSamplingContext: DynamicSamplingContext{},
	}
	newDsc := DynamicSamplingContext{
		Entries: map[string]string{"environment": "dev"},
		Frozen:  true,
	}

	s.SetDynamicSamplingContext(newDsc)

	if diff := cmp.Diff(DynamicSamplingContext{}, s.dynamicSamplingContext); diff != "" {
		t.Errorf("DynamicSamplingContext mismatch (-want +got):\n%s", diff)
	}
}

func TestParseTraceParentContext(t *testing.T) {
	tests := []struct {
		name        string
		sentryTrace string
		wantContext TraceParentContext
		wantValid   bool
	}{
		{
			name:        "Malformed header",
			sentryTrace: "xxx-malformed",
			wantContext: TraceParentContext{},
			wantValid:   false,
		},
		{
			name:        "Valid header, sampled",
			sentryTrace: "d49d9bf66f13450b81f65bc51cf49c03-1cc4b26ab9094ef0-1",
			wantContext: TraceParentContext{
				TraceID:      TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
				ParentSpanID: SpanIDFromHex("1cc4b26ab9094ef0"),
				Sampled:      SampledTrue,
			},
			wantValid: true,
		},
		{
			name:        "Valid header, unsampled",
			sentryTrace: "d49d9bf66f13450b81f65bc51cf49c03-1cc4b26ab9094ef0-0",
			wantContext: TraceParentContext{
				TraceID:      TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03"),
				ParentSpanID: SpanIDFromHex("1cc4b26ab9094ef0"),
				Sampled:      SampledFalse,
			},
			wantValid: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			traceParentContext, valid := ParseTraceParentContext([]byte(tt.sentryTrace))

			if diff := cmp.Diff(tt.wantContext, traceParentContext); diff != "" {
				t.Errorf("Context mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantValid, valid); diff != "" {
				t.Errorf("Context validity mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetTransactionWithProperTransactionsSpans(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	transaction := StartTransaction(ctx, "transaction")
	child1 := transaction.StartChild("child1")
	child2 := transaction.StartChild("child2")
	grandchild := child1.StartChild("grandchild")

	assertEqual(t, transaction.GetTransaction(), transaction)
	assertEqual(t, child1.GetTransaction(), transaction)
	assertEqual(t, child2.GetTransaction(), transaction)
	assertEqual(t, grandchild.GetTransaction(), transaction)

	// Another transaction, unrelated to the first one
	anotherTransaction := StartTransaction(ctx, "another transaction")

	assertNotEqual(t, transaction, anotherTransaction)
	assertEqual(t, anotherTransaction.GetTransaction(), anotherTransaction)
}

func TestGetTransactionReturnsNilOnManuallyCreatedSpans(t *testing.T) {
	span1 := Span{}
	if span1.GetTransaction() != nil {
		t.Errorf("GetTransaction() should return nil on manually created Spans")
	}

	span2 := Span{}
	if span2.GetTransaction() != nil {
		t.Errorf("GetTransaction() should return nil on manually created Spans")
	}
}

func TestToBaggage(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Release:          "test-release",
	})
	transaction := StartTransaction(ctx, "transaction-name")
	transaction.TraceID = TraceIDFromHex("f1a4c5c9071eca1cdf04e4132527ed16")

	assertBaggageStringsEqual(
		t,
		transaction.ToBaggage(),
		"sentry-trace_id=f1a4c5c9071eca1cdf04e4132527ed16,sentry-release=test-release,sentry-transaction=transaction-name,sentry-sample_rate=1,sentry-sampled=true",
	)

	// Calling ToBaggage() on a child span should return the same result
	child := transaction.StartChild("op-name")
	assertBaggageStringsEqual(
		t,
		child.ToBaggage(),
		"sentry-trace_id=f1a4c5c9071eca1cdf04e4132527ed16,sentry-release=test-release,sentry-transaction=transaction-name,sentry-sample_rate=1,sentry-sampled=true",
	)
}

func TestSpanSetContext(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	transaction := StartTransaction(ctx, "Test Transaction")

	transaction.SetContext("a", Context{"b": 1})

	assertEqual(t, map[string]Context{"a": {"b": 1}}, transaction.contexts)
}

func TestSpanSetContextMerges(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	transaction := StartTransaction(ctx, "Test Transaction")
	transaction.SetContext("a", Context{"foo": "bar"})
	transaction.SetContext("b", Context{"b": 2})

	assertEqual(t, map[string]Context{"a": {"foo": "bar"}, "b": {"b": 2}}, transaction.contexts)
}

func TestSpanSetContextOverrides(t *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing: true,
	})
	transaction := StartTransaction(ctx, "Test Transaction")
	transaction.SetContext("a", Context{"foo": "bar"})
	transaction.SetContext("a", Context{"foo": 2})

	assertEqual(t, map[string]Context{"a": {"foo": 2}}, transaction.contexts)
}

// This test checks that there are no concurrent reads/writes to
// substructures in scope.contexts.
// See https://github.com/getsentry/sentry-go/issues/570 for more details.
func TestConcurrentContextAccess(_ *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1,
	})
	hub := GetHubFromContext(ctx)

	const writersNum = 200

	// Unbuffered channel, writing to it will be block if nobody reads
	c := make(chan *Span)

	// Start writers
	for i := 0; i < writersNum; i++ {
		go func() {
			transaction := StartTransaction(ctx, "test")
			c <- transaction
			hub.Scope().SetContext("device", Context{"test": "bla"})
		}()
	}

	var wg sync.WaitGroup
	wg.Add(writersNum)

	// Start readers
	go func() {
		for transaction := range c {
			transaction := transaction
			go func() {
				defer wg.Done()
				// While finalizing every transaction, scope.Contexts and Event.Contexts fields
				// will be accessed, e.g. in environmentIntegration.processor()
				transaction.Finish()
			}()
		}
	}()

	wg.Wait()
}

func TestAdjustingTransactionSourceBeforeSending(t *testing.T) {
	tests := []struct {
		name                   string
		inputTransactionSource TransactionSource
		wantTransactionSource  TransactionSource
	}{
		{
			name:                   "Invalid transaction source",
			inputTransactionSource: "invalidSource",
			wantTransactionSource:  "custom",
		},
		{
			name:                   "Valid transaction source",
			inputTransactionSource: SourceTask,
			wantTransactionSource:  "task",
		},
		{
			name:                   "Empty transaction source",
			inputTransactionSource: "",
			wantTransactionSource:  "custom",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			transport := &MockTransport{}
			ctx := NewTestContext(ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				Transport:        transport,
			})
			transaction := StartTransaction(
				ctx,
				"Test Transaction",
				WithTransactionSource(tt.inputTransactionSource),
			)
			transaction.Finish()

			event := transport.Events()[0]

			assertEqual(t, event.TransactionInfo.Source, tt.wantTransactionSource)
		})
	}
}

// This is a regression test for https://github.com/getsentry/sentry-go/issues/587
// Without the "spans can be finished only once" fix, this test will fail
// when run with race detection ("-race").
func TestSpanFinishConcurrentlyWithoutRaces(_ *testing.T) {
	ctx := NewTestContext(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1,
	})
	transaction := StartTransaction(ctx, "op")

	go func() {
		for {
			transaction.Finish()
		}
	}()

	go func() {
		for {
			transaction.Finish()
		}
	}()

	time.Sleep(50 * time.Millisecond)
}

func TestSpanScopeManagement(t *testing.T) {
	// Initialize a test hub and client
	transport := &MockTransport{}
	client, err := newClient(ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		Transport:        transport,
	})
	if err != nil {
		t.Fatal(err)
	}
	hub := NewHub(client, NewScope())

	// Set the hub on the context
	ctx := context.Background()
	ctx = SetHubOnContext(ctx, hub)

	// Start a parent span (transaction)
	transaction := StartTransaction(ctx, "parent-operation")
	defer transaction.Finish()

	// Start a child span
	childSpan := StartSpan(transaction.Context(), "child-operation")
	// Finish the child span
	defer childSpan.Finish()

	subChildSpan := StartSpan(childSpan.Context(), "sub_child-operation")
	subChildSpan.Finish()

	// Capture an event after finishing the child span
	// This event should be associated with the first child span
	hub.CaptureMessage("Test event")

	// Flush to ensure the event is sent
	transport.Flush(time.Second)

	// Verify that the event has the correct trace data
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 2 event, got %d", len(events))
	}
	event := events[0]

	// Extract the trace context from the event
	traceCtx, ok := event.Contexts["trace"]
	if !ok {
		t.Fatalf("event does not have a trace context")
	}

	// Extract TraceID and SpanID from the trace context
	traceID, ok := traceCtx["trace_id"].(TraceID)
	if !ok {
		t.Fatalf("trace_id not found")
	}
	spanID, ok := traceCtx["span_id"].(SpanID)
	if !ok {
		t.Fatalf("span_id not found")
	}

	// Verify that the IDs match the first child span IDs
	if traceID != childSpan.TraceID {
		t.Errorf("expected TraceID %s, got %s", transaction.TraceID, traceID)
	}
	if spanID != childSpan.SpanID {
		t.Errorf("expected SpanID %s, got %s", transaction.SpanID, spanID)
	}
}

func TestTraceResolutionPrecedence(t *testing.T) {
	client, err := newClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	carried := PropagationContext{
		TraceID:      TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		SpanID:       SpanIDFromHex("bbbbbbbbbbbbbbbb"),
		ParentSpanID: SpanIDFromHex("1111111111111111"),
		Sampled:      SampledFalse,
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"source": "carried"},
			Frozen:  true,
		},
	}
	ctx := contextWithPropagationContext(context.Background(), carried)

	external := PropagationContext{
		TraceID:      TraceIDFromHex("cccccccccccccccccccccccccccccccc"),
		SpanID:       SpanIDFromHex("dddddddddddddddd"),
		ParentSpanID: SpanIDFromHex("2222222222222222"),
		Sampled:      SampledTrue,
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"source": "external"},
			Frozen:  true,
		},
	}
	client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
		return external, true
	})

	got, ok := propagationContextFromContext(ctx, client)
	assert.True(t, ok)
	assert.Equal(t, external, got)

	native := &Span{
		TraceID:      TraceIDFromHex("eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		SpanID:       SpanIDFromHex("ffffffffffffffff"),
		ParentSpanID: SpanIDFromHex("3333333333333333"),
		Sampled:      SampledTrue,
		recorder:     &spanRecorder{},
	}
	native.recorder.record(native)
	nativeCtx := context.WithValue(ctx, spanContextKey{}, native)
	got, ok = propagationContextFromContext(nativeCtx, client)
	assert.True(t, ok)
	assert.Equal(t, native.TraceID, got.TraceID)
	assert.Equal(t, native.SpanID, got.SpanID)
	assert.Equal(t, native.ParentSpanID, got.ParentSpanID)

	client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
		return PropagationContext{}, true
	})
	got, ok = propagationContextFromContext(ctx, client)
	assert.True(t, ok)
	assert.Equal(t, carried, got)
}

func TestExternalResolverDoesNotBorrowCarriedDSC(t *testing.T) {
	transport := &MockTransport{}
	client, err := newClient(ClientOptions{
		Dsn:       "https://key@example.com/1",
		Transport: transport,
	})
	if err != nil {
		t.Fatal(err)
	}

	carried := PropagationContext{
		TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		SpanID:  SpanIDFromHex("bbbbbbbbbbbbbbbb"),
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"release": "carried"},
			Frozen:  true,
		},
	}
	external := PropagationContext{
		TraceID: TraceIDFromHex("cccccccccccccccccccccccccccccccc"),
		SpanID:  SpanIDFromHex("dddddddddddddddd"),
		DynamicSamplingContext: DynamicSamplingContext{
			Frozen: true,
		},
	}
	client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
		return external, true
	})
	ctx := contextWithPropagationContext(context.Background(), carried)
	ctx, scope := ScopeFromContext(ctx)
	scope.SetClient(client)

	client.CaptureMessage(ctx, "external")
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	trace := events[0].Contexts["trace"]
	assert.Equal(t, external.TraceID, trace["trace_id"])
	assert.Equal(t, external.SpanID, trace["span_id"])
	assert.Empty(t, events[0].sdkMetaData.dsc.Entries)
	assert.True(t, events[0].sdkMetaData.dsc.Frozen)
}

func TestHeaderResolution(t *testing.T) {
	t.Run("external source without native span", func(t *testing.T) {
		client, err := newClient(ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		external := PropagationContext{
			TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
			SpanID:  SpanIDFromHex("bbbbbbbbbbbbbbbb"),
			Sampled: SampledTrue,
			DynamicSamplingContext: DynamicSamplingContext{
				Entries: map[string]string{"release": "external"},
				Frozen:  true,
			},
		}
		client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
			return external, true
		})
		ctx, scope := ScopeFromContext(context.Background())
		scope.SetClient(client)

		assert.Equal(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-1", GetTraceparent(ctx))
		assert.Equal(t, "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01", GetTraceparentW3C(ctx))
		assertBaggageStringsEqual(t, "sentry-release=external", GetBaggage(ctx))
	})

	t.Run("native span wins over external source", func(t *testing.T) {
		client, err := newClient(ClientOptions{})
		if err != nil {
			t.Fatal(err)
		}
		client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
			return PropagationContext{
				TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
				SpanID:  SpanIDFromHex("bbbbbbbbbbbbbbbb"),
			}, true
		})
		ctx, scope := ScopeFromContext(context.Background())
		scope.SetClient(client)
		native := &Span{
			TraceID:  TraceIDFromHex("cccccccccccccccccccccccccccccccc"),
			SpanID:   SpanIDFromHex("dddddddddddddddd"),
			Sampled:  SampledFalse,
			recorder: &spanRecorder{},
		}
		native.recorder.record(native)
		ctx = context.WithValue(ctx, spanContextKey{}, native)

		assert.Equal(t, "cccccccccccccccccccccccccccccccc-dddddddddddddddd-0", GetTraceparent(ctx))
		assert.Equal(t, "00-cccccccccccccccccccccccccccccccc-dddddddddddddddd-00", GetTraceparentW3C(ctx))
	})
}

func TestExternalResolverSnapshotDoesNotAliasDSC(t *testing.T) {
	client, err := newClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	external := PropagationContext{
		TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		SpanID:  SpanIDFromHex("bbbbbbbbbbbbbbbb"),
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"release": "original"},
			Frozen:  true,
		},
	}
	client.SetExternalContextTraceResolver(func(context.Context) (PropagationContext, bool) {
		return external, true
	})

	first, ok := client.externalPropagationContextFromContext(context.Background())
	assert.True(t, ok)
	first.DynamicSamplingContext.Entries["release"] = "mutated"
	second, ok := client.externalPropagationContextFromContext(context.Background())
	assert.True(t, ok)
	assert.Equal(t, "original", second.DynamicSamplingContext.Entries["release"])
}

func TestStrictTraceContinuation(t *testing.T) {
	incomingTraceID := TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4")
	sentryTrace := "bc6d53f15eb88f4320054569b8c553d4-b72fa28504b07285-1"

	baggageWithOrg := func(orgID string) string {
		return "sentry-org_id=" + orgID + ",sentry-trace_id=bc6d53f15eb88f4320054569b8c553d4"
	}
	baggageWithoutOrg := "sentry-trace_id=bc6d53f15eb88f4320054569b8c553d4"

	tests := []struct {
		name          string
		baggageOrgID  string
		sdkOrgID      uint64
		strict        bool
		wantContinued bool
	}{
		{"strict=false, baggage=1, sdk=1", "1", 1, false, true},
		{"strict=false, baggage=none, sdk=1", "", 1, false, true},
		{"strict=false, baggage=1, sdk=none", "1", 0, false, true},
		{"strict=false, baggage=none, sdk=none", "", 0, false, true},
		{"strict=false, baggage=1, sdk=2", "1", 2, false, false},

		{"strict=true, baggage=1, sdk=1", "1", 1, true, true},
		{"strict=true, baggage=none, sdk=1", "", 1, true, false},
		{"strict=true, baggage=1, sdk=none", "1", 0, true, false},
		{"strict=true, baggage=none, sdk=none", "", 0, true, true},
		{"strict=true, baggage=1, sdk=2", "1", 2, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &MockTransport{}
			ctx := NewTestContext(ClientOptions{
				Dsn:                     testDsn,
				EnableTracing:           true,
				TracesSampleRate:        1.0,
				Transport:               transport,
				StrictTraceContinuation: tt.strict,
				OrgID:                   tt.sdkOrgID,
			})

			baggage := baggageWithoutOrg
			if tt.baggageOrgID != "" {
				baggage = baggageWithOrg(tt.baggageOrgID)
			}

			ctx = ContinueTrace(ctx, sentryTrace, baggage)
			transaction := StartTransaction(ctx, "test")
			transaction.Finish()

			if tt.wantContinued {
				if transaction.TraceID != incomingTraceID {
					t.Errorf("expected trace to be continued, got new TraceID %s", transaction.TraceID)
				}
			} else {
				if transaction.TraceID == incomingTraceID {
					t.Errorf("expected new trace, but got continued TraceID %s", transaction.TraceID)
				}
			}
		})
	}
}
