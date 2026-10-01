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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testExternalResolverFunc func(context.Context) (TraceID, SpanID, Sampled, bool)

func (resolver testExternalResolverFunc) ResolveTraceContext(ctx context.Context) (TraceID, SpanID, Sampled, bool) {
	return resolver(ctx)
}

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

	t.Run("does not replace transaction context", func(t *testing.T) {
		tr := StartTransaction(context.TODO(), "")
		ctx := context.WithValue(tr.Context(), testContextKey{}, testContextValue{})
		existingTr := StartTransaction(ctx, "")
		_, keyExists := existingTr.Context().Value(testContextKey{}).(testContextValue)
		if keyExists {
			t.Fatalf("transaction context was replaced")
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
	client, err := NewClient(options)
	if err != nil {
		panic(err)
	}
	ctx := context.WithValue(context.Background(), testContextKey{}, testContextValue{})
	ctx, _ = WithIsolationScope(ctx)
	return ContextWithClient(ctx, client)
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

func TestContinueSpanFromRequest(t *testing.T) {
	traceID := TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4")
	spanID := SpanIDFromHex("b72fa28504b07285")

	for _, sampled := range []Sampled{SampledTrue, SampledFalse, SampledUndefined} {
		sampled := sampled
		t.Run(sampled.String(), func(t *testing.T) {
			var s Span
			s.ctx = context.Background()
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

func TestContinueSpanFromTrace(t *testing.T) {
	traceID := TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4")
	spanID := SpanIDFromHex("b72fa28504b07285")

	for _, sampled := range []Sampled{SampledTrue, SampledFalse, SampledUndefined} {
		sampled := sampled
		t.Run(sampled.String(), func(t *testing.T) {
			s := &Span{}
			s.ctx = context.Background()
			trace := (&Span{
				TraceID: traceID,
				SpanID:  spanID,
				Sampled: sampled,
			}).ToSentryTrace()
			ContinueFromTrace(trace)(s)
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

func TestContinueTrace(t *testing.T) {
	tests := []struct {
		name       string
		traceStr   string
		baggageStr string
		// Using a pointer to Span so we don't implicitly copy Span.mu mutex
		wantSpan *Span
	}{
		{
			name:       "No sentry-trace or baggage => nothing to do, unfrozen DSC",
			traceStr:   "",
			baggageStr: "",
			wantSpan: &Span{
				Sampled: 0,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen:  false,
					Entries: nil,
				},
			},
		},
		{
			name:       "baggage => nothing to do, unfrozen DSC",
			traceStr:   "",
			baggageStr: "other-vendor-key1=value1;value2, other-vendor-key2=value3",
			wantSpan: &Span{
				Sampled: 0,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen:  false,
					Entries: nil,
				},
			},
		},
		{
			name:       "sentry-trace and no baggage => we should create a new DSC and freeze it",
			traceStr:   "bc6d53f15eb88f4320054569b8c553d4-b72fa28504b07285-1",
			baggageStr: "",
			wantSpan: &Span{
				TraceID:      TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
				ParentSpanID: SpanIDFromHex("b72fa28504b07285"),
				Sampled:      1,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen: true,
				},
			},
		},
		{
			name:       "sentry-trace and malformed baggage => continue with empty frozen DSC",
			traceStr:   "bc6d53f15eb88f4320054569b8c553d4-b72fa28504b07285-1",
			baggageStr: "invalid baggage @@@",
			wantSpan: &Span{
				TraceID:      TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
				ParentSpanID: SpanIDFromHex("b72fa28504b07285"),
				Sampled:      1,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen: true,
				},
			},
		},
		{
			name:       "sentry-trace and baggage with Sentry values => we freeze immediately.",
			traceStr:   "bc6d53f15eb88f4320054569b8c553d4-b72fa28504b07285-1",
			baggageStr: "sentry-trace_id=bc6d53f15eb88f4320054569b8c553d4,sentry-public_key=public,sentry-sample_rate=1",
			wantSpan: &Span{
				TraceID:      TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
				ParentSpanID: SpanIDFromHex("b72fa28504b07285"),
				Sampled:      1,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen: true,
					Entries: map[string]string{
						"public_key":  "public",
						"sample_rate": "1",
						"trace_id":    "bc6d53f15eb88f4320054569b8c553d4",
					},
				},
			},
		},
		{
			name:       "conflicting DSC retains the incoming trace",
			traceStr:   "bc6d53f15eb88f4320054569b8c553d4-b72fa28504b07285-1",
			baggageStr: "sentry-trace_id=11111111111111111111111111111111,sentry-sample_rate=0.25",
			wantSpan: &Span{
				TraceID:                TraceIDFromHex("bc6d53f15eb88f4320054569b8c553d4"),
				ParentSpanID:           SpanIDFromHex("b72fa28504b07285"),
				Sampled:                SampledTrue,
				dynamicSamplingContext: DynamicSamplingContext{Frozen: true},
			},
		},
		{
			name:       "no sentry-trace and baggage with Sentry values => unfrozen DSC",
			baggageStr: "sentry-trace_id=d49d9bf66f13450b81f65bc51cf49c03,sentry-public_key=public,sentry-sample_rate=1",
			wantSpan: &Span{
				Sampled: 0,
				dynamicSamplingContext: DynamicSamplingContext{
					Frozen:  false,
					Entries: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Span{}
			s.ctx = context.Background()
			spanOption := ContinueTrace(tt.traceStr, tt.baggageStr)
			spanOption(s)
			if tt.wantSpan.dynamicSamplingContext.IsFrozen() && !tt.wantSpan.dynamicSamplingContext.HasEntries() {
				require.Empty(t, s.ToBaggage())
			}

			if diff := cmp.Diff(tt.wantSpan, s, cmp.Options{
				cmp.AllowUnexported(Span{}),
				cmpopts.IgnoreFields(Span{}, "ctx", "mu", "finishOnce", "serializationSafe"),
			}); diff != "" {
				t.Fatalf("Expected no difference on spans, got: %s", diff)
			}
		})
	}
}

func TestSpanFromContextWithoutSpan(t *testing.T) {
	t.Parallel()

	assert.Nil(t, SpanFromContext(context.Background()))
	assert.Nil(t, SpanFromContext(nil)) //nolint:staticcheck // Verify the nil-context fallback.
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
	CaptureException(ctx, errors.New("ignored"))
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
	if got := span.Sampled; got != SampledUndefined {
		t.Fatalf("got %s, want %s", got, SampledUndefined)
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
				"sentry-sample_rate=1",
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
				"sentry-sample_rate=0.0",
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

			options := []SpanOption{
				ContinueTrace(tt.traceHeader, tt.baggageHeader),
			}
			transaction := StartTransaction(ctx, "test-transaction", options...)
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
	t.Run("scope captures retain root sampling metadata", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			option SpanOption
		}{
			{name: "sampled"},
			{name: "unsampled", option: WithSpanSampled(SampledFalse)},
			{name: "incoming baggage", option: ContinueFromHeaders("11111111111111111111111111111111-2222222222222222-1", "sentry-trace_id=11111111111111111111111111111111,sentry-public_key=upstream,sentry-sampled=true")},
			{name: "frozen empty baggage", option: ContinueFromTrace("11111111111111111111111111111111-2222222222222222-1")},
		} {
			t.Run(test.name, func(t *testing.T) {
				client, transport := newCaptureTestClient(t, ClientOptions{EnableTracing: true, TracesSampleRate: 1, Release: "scope-release"})
				ctx, scope := WithIsolationScope(ContextWithClient(context.Background(), client))
				options := []SpanOption{}
				if test.option != nil {
					options = append(options, test.option)
				}
				root := StartTransaction(ctx, "root", options...)
				want := root.dynamicSamplingContextForPropagation()
				require.Same(t, root, scope.GetSpan())
				require.NotNil(t, CaptureMessage(ctx, "before"))
				root.Finish()
				require.NotNil(t, CaptureMessage(ctx, "after"))
				var count int
				for _, event := range transport.Events() {
					if event.Type != transactionType {
						count++
						require.Equal(t, want, event.sdkMetaData.dsc)
						require.Equal(t, root.TraceID, event.Contexts[traceContextKey][traceIDContextKey])
					}
				}
				require.Equal(t, 2, count)
			})
		}
	})

	t.Run("disabled tracing preserves incoming decisions", func(t *testing.T) {
		for _, test := range []struct {
			name, header string
			want         Sampled
		}{
			{name: "true", header: "11111111111111111111111111111111-2222222222222222-1", want: SampledTrue},
			{name: "false", header: "11111111111111111111111111111111-2222222222222222-0", want: SampledFalse},
			{name: "undefined", header: "11111111111111111111111111111111-2222222222222222", want: SampledUndefined},
			{name: "fresh", want: SampledUndefined},
		} {
			t.Run(test.name, func(t *testing.T) {
				client, transport := newCaptureTestClient(t, ClientOptions{EnableTracing: false})
				ctx, _ := WithIsolationScope(ContextWithClient(context.Background(), client))
				root := StartTransaction(ctx, "disabled", ContinueTrace(test.header, ""))
				require.Equal(t, test.want, root.Sampled)
				root.Finish()
				require.Empty(t, transport.Events())
				require.Nil(t, client.reportProvider.TakeReport())
			})
		}
	})

	t.Run("child inherits the parent decision across client changes", func(t *testing.T) {
		enabled, _ := newCaptureTestClient(t, ClientOptions{EnableTracing: true, TracesSampleRate: 1})
		disabled, _ := newCaptureTestClient(t, ClientOptions{EnableTracing: false})
		ctx, _ := WithIsolationScope(ContextWithClient(context.Background(), enabled))
		root := StartTransaction(ctx, "root")
		child := StartSpan(ContextWithClient(root.Context(), disabled), "child", WithSpanSampled(SampledFalse))
		require.Equal(t, SampledTrue, child.Sampled)
	})
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

			txn := StartTransaction(ctx, "test-txn", ContinueTrace(tt.traceHeader, tt.baggageHeader))
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

	// The generated DSC is frozen on first propagation, so later replacement is ignored.
	transaction.SetDynamicSamplingContext(DynamicSamplingContext{
		Entries: map[string]string{"release": "incoming-release"},
		Frozen:  true,
	})
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
	scope := ScopeFromContext(ctx)

	const writersNum = 200

	// Unbuffered channel, writing to it will be block if nobody reads
	c := make(chan *Span)

	// Start writers
	for i := 0; i < writersNum; i++ {
		go func() {
			transaction := StartTransaction(ctx, "test")
			c <- transaction
			scope.SetContext("device", Context{"test": "bla"})
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

func TestSpanScopeIsNotActiveSpanStack(t *testing.T) {
	client, transport := newCaptureTestClient(t, ClientOptions{EnableTracing: true, TracesSampleRate: 1})
	ctx, scope := WithIsolationScope(context.Background())
	ctx = ContextWithClient(ctx, client)

	transaction := StartTransaction(ctx, "parent-operation")
	require.Same(t, transaction, scope.GetSpan())
	traceID, _ := resolveTrace(scope, client, ctx)
	require.Equal(t, transaction.TraceID, traceID)

	childSpan := StartSpan(transaction.Context(), "child-operation")
	siblingSpan := StartSpan(transaction.Context(), "sibling-operation")
	subChildSpan := StartSpan(childSpan.Context(), "sub_child-operation")
	childSpan.Finish()
	siblingSpan.Finish()
	subChildSpan.Finish()

	CaptureMessage(childSpan.Context(), "Test event")

	trace := requireSingleEvent(t, transport).Contexts[traceContextKey]
	require.Equal(t, childSpan.TraceID, trace[traceIDContextKey])
	require.Equal(t, childSpan.SpanID, trace[spanIDContextKey])
	transaction.Finish()
	require.Same(t, transaction, scope.GetSpan())
}

func TestContextPropagationHeaders(t *testing.T) {
	t.Run("global propagation context", func(t *testing.T) {
		client, _ := newCaptureTestClient(t, ClientOptions{})
		ctx := ContextWithClient(context.Background(), client)
		scope := cleanGlobalScope(t)
		scope.SetPropagationContext(NewPropagationContext())
		propagation := scope.propagationContextSnapshot()
		traceparent := propagation.TraceID.String() + "-" + propagation.SpanID.String()
		require.Equal(t, traceparent, GetTraceparent(ctx))
		require.Equal(t, "00-"+traceparent+"-00", GetTraceparentW3C(ctx))
		assertBaggageStringsEqual(t, GetBaggage(ctx), DynamicSamplingContextFromScope(scope, client).String())
	})

	t.Run("scope propagation context", func(t *testing.T) {
		client, _ := newCaptureTestClient(t, ClientOptions{EnableTracing: true})
		traceID := TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		spanID := SpanIDFromHex("bbbbbbbbbbbbbbbb")
		for _, test := range []struct {
			name, suffix, flags, release string
			sampled                      Sampled
			staticZero                   bool
		}{
			{name: "undefined", flags: "00", release: "scope-release"},
			{name: "sampled", suffix: "-1", flags: "01", release: "scope-release", sampled: SampledTrue},
			{name: "unsampled", suffix: "-0", flags: "00", release: "scope-release", sampled: SampledFalse},
			{name: "frozen empty", flags: "00"},
			{name: "static zero deferred", suffix: "-0", flags: "00", staticZero: true},
			{name: "static zero sampled", suffix: "-1", flags: "01", sampled: SampledTrue, staticZero: true},
			{name: "static zero unsampled", suffix: "-0", flags: "00", sampled: SampledFalse, staticZero: true},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx, scope := WithIsolationScope(context.Background())
				dsc := DynamicSamplingContext{Frozen: true}
				if test.release != "" {
					dsc.Entries = map[string]string{"release": test.release}
				}
				propagation := PropagationContext{TraceID: traceID, SpanID: spanID, Sampled: test.sampled, DynamicSamplingContext: dsc}
				if test.staticZero {
					ctx = ContextWithClient(ctx, client)
					propagation.ParentSpanID = SpanID{3}
					dsc.Entries = map[string]string{traceIDContextKey: traceID.String(), "public_key": "upstream"}
					propagation.DynamicSamplingContext = dsc
				}
				scope.SetPropagationContext(propagation)
				ids := traceID.String() + "-" + spanID.String()
				require.Equal(t, ids+test.suffix, GetTraceparent(ctx))
				require.Equal(t, "00-"+ids+"-"+test.flags, GetTraceparentW3C(ctx))
				assertBaggageStringsEqual(t, GetBaggage(ctx), dsc.String())
				wantDecision := test.sampled
				if test.staticZero && wantDecision == SampledUndefined {
					wantDecision = SampledFalse
				}
				require.Equal(t, wantDecision, scope.propagationContextSnapshot().Sampled)
			})
		}
	})

	t.Run("static zero sample rate", func(t *testing.T) {
		creator, _ := newCaptureTestClient(t, ClientOptions{EnableTracing: true, Release: "creator"})
		replacement, replacementTransport := newCaptureTestClient(t, ClientOptions{EnableTracing: true, Release: "replacement"})
		ctx, scope := WithIsolationScope(ContextWithClient(context.Background(), creator))

		require.Equal(t, SampledUndefined, scope.propagationContextSnapshot().Sampled)
		require.True(t, strings.HasSuffix(GetTraceparent(ctx), "-0"))
		require.True(t, strings.HasSuffix(GetTraceparentW3C(ctx), "-00"))
		require.False(t, scope.propagationContextSnapshot().DynamicSamplingContext.IsFrozen())

		ctx = ContextWithClient(ctx, replacement)
		baggage := GetBaggage(ctx)
		require.Contains(t, baggage, "sentry-release=replacement")
		require.Contains(t, baggage, "sentry-sampled=false")
		require.Contains(t, baggage, "sentry-sample_rate=0")
		require.NotNil(t, CaptureMessage(ctx, "static zero"))
		dsc := requireSingleEvent(t, replacementTransport).sdkMetaData.dsc
		require.Equal(t, SampledFalse, scope.propagationContextSnapshot().Sampled)
		require.Equal(t, "false", dsc.Entries["sampled"])
		require.Equal(t, "0", dsc.Entries["sample_rate"])
	})

	t.Run("deferred external trace and invalid fallback", func(t *testing.T) {
		client, _ := newCaptureTestClient(t, ClientOptions{})
		ctx, scope := WithIsolationScope(ContextWithClient(context.Background(), client))
		externalTraceID, externalSpanID := TraceID{1}, SpanID{2}
		client.externalTraceResolver = testExternalResolverFunc(func(ctx context.Context) (TraceID, SpanID, Sampled, bool) {
			if ctx.Value(spanContextKey{}) != nil {
				return TraceID{}, externalSpanID, SampledUndefined, true
			}
			return externalTraceID, externalSpanID, SampledUndefined, true
		})
		require.Equal(t, externalTraceID.String()+"-"+externalSpanID.String(), GetTraceparent(ctx))
		require.Equal(t, "00-"+externalTraceID.String()+"-"+externalSpanID.String()+"-00", GetTraceparentW3C(ctx))
		root := StartTransaction(ctx, "native")
		defer root.Finish()
		require.Equal(t, root.TraceID.String()+"-"+root.SpanID.String(), GetTraceparent(root.Context()))
		require.Equal(t, SampledUndefined, scope.propagationContextSnapshot().Sampled)
	})

	for _, test := range []struct {
		name          string
		release       string
		enableTracing bool
		wantSampled   Sampled
	}{
		{name: "disabled tracing", release: "disabled-tracing", enableTracing: false, wantSampled: SampledUndefined},
		{name: "custom client", release: "custom-client", enableTracing: true, wantSampled: SampledTrue},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				Dsn:              testDsn,
				EnableTracing:    test.enableTracing,
				TracesSampleRate: 1,
				Release:          test.release,
				Transport:        &MockTransport{},
			})
			require.NoError(t, err)
			ctx, _ := WithIsolationScope(context.Background())
			transaction := StartTransaction(ContextWithClient(ctx, client), "transaction")
			require.Equal(t, test.wantSampled, transaction.Sampled)
			require.Equal(t, transaction.ToSentryTrace(), GetTraceparent(transaction.Context()))
			require.Equal(t, transaction.ToTraceparent(), GetTraceparentW3C(transaction.Context()))
			require.Contains(t, GetBaggage(transaction.Context()), "sentry-release="+test.release)
		})
	}
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

			transaction := StartTransaction(ctx, "test",
				ContinueTrace(sentryTrace, baggage),
			)
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

func TestGetBaggageForExternalTrace(t *testing.T) {
	t.Parallel()

	externalTraceID := TraceID{1}
	matching := DynamicSamplingContext{Frozen: true, Entries: map[string]string{
		"trace_id": externalTraceID.String(), "public_key": "upstream", "sampled": "true",
	}}
	for _, test := range []struct {
		name string
		dsc  DynamicSamplingContext
		want string
	}{
		{name: "matching trace", dsc: matching, want: matching.String()},
		{name: "different trace", dsc: DynamicSamplingContext{Frozen: true, Entries: map[string]string{
			"trace_id": TraceID{2}.String(), "public_key": "upstream", "sampled": "true",
		}}},
		{name: "missing trace ID", dsc: DynamicSamplingContext{Frozen: true, Entries: map[string]string{"public_key": "upstream"}}},
		{name: "frozen empty DSC", dsc: DynamicSamplingContext{Frozen: true}},
		{name: "no DSC"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client, _ := newCaptureTestClient(t, ClientOptions{})
			client.externalTraceResolver = testExternalResolverFunc(func(context.Context) (TraceID, SpanID, Sampled, bool) {
				return externalTraceID, SpanID{3}, SampledUndefined, true
			})
			scope := NewScope()
			scope.SetPropagationContext(PropagationContext{TraceID: externalTraceID, SpanID: SpanID{4}, DynamicSamplingContext: test.dsc})
			ctx := ContextWithClient(ContextWithScope(context.Background(), scope), client)
			before := scope.propagationContextSnapshot()

			assertBaggageStringsEqual(t, GetBaggage(ctx), test.want)
			require.Equal(t, externalTraceID.String()+"-"+(SpanID{3}).String(), GetTraceparent(ctx))
			require.Equal(t, before, scope.propagationContextSnapshot())
		})
	}
}
