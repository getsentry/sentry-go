package sentry

import (
	"context"
	"testing"
)

func TestStartSpan(t *testing.T) {
	ctx := NewTestContext(ClientOptions{})
	span := StartSpan(ctx, "top", WithTransactionName("Test Transaction"))
	span.Finish()

	SpanCheck{
		RecorderLen: 1,
	}.Check(t, span)

	// TODO calling Finish sets the span EndTime (and every span
	// has StartTime set?)
}

func TestStartChild(t *testing.T) {
	ctx := NewTestContext(ClientOptions{})
	span := StartSpan(ctx, "top", WithTransactionName("Test Transaction"))
	child := span.StartChild("child")
	child.Finish()
	span.Finish()

	c := SpanCheck{
		RecorderLen: 2,
	}
	c.Check(t, span)
	c.Check(t, child)
}

func TestSpanFromContext(t *testing.T) {
	// SpanFromContext always returns a non-nil value, such that you can use
	// it without nil checks.
	// When no span was in the context, the returned value is a no-op.
	// Calling StartChild on the no-op creates a valid transaction.
	// SpanFromContext(ctx).StartChild(...) === StartSpan(ctx, ...)

	ctx := NewTestContext(ClientOptions{})
	span := SpanFromContext(ctx)

	SpanCheck{
		ZeroTraceID: true,
		ZeroSpanID:  true,
	}.Check(t, span)

	// Should create a transaction
	child := span.StartChild("top")
	SpanCheck{
		RecorderLen: 1,
	}.Check(t, child)

	// TODO: check behavior of Finishing sending transactions to Sentry
}

// testSecretContextKey is used to store a "secret value" in a context so that
// we can check that operations on that context preserves our original context.
type testSecretContextKey struct{}
type testSecretContextValue struct{}

func NewTestContext(options ClientOptions) context.Context {
	client, err := NewClient(options)
	if err != nil {
		panic(err)
	}
	hub := NewHub(client, NewScope())
	ctx := context.WithValue(context.Background(), testSecretContextKey{}, testSecretContextValue{})
	return SetHubOnContext(ctx, hub)
}

// Zero values of TraceID and SpanID used for comparisons.
var (
	zeroTraceID TraceID
	zeroSpanID  SpanID
)

// A SpanCheck is a test helper describing span properties that can be checked
// with the Check method.
type SpanCheck struct {
	Sampled     bool
	ZeroTraceID bool
	ZeroSpanID  bool
	RecorderLen int
}

func (c SpanCheck) Check(t *testing.T, span Span) {
	t.Helper()

	// Invariant: context preservation
	gotCtx := span.Context()
	if _, ok := gotCtx.Value(testSecretContextKey{}).(testSecretContextValue); !ok {
		t.Errorf("original context lost")
	}
	// Invariant: SpanFromContext(span.Context) == span
	if SpanFromContext(gotCtx) != span {
		t.Errorf("span not in its context")
	}

	spanContext := span.SpanContext()
	if got := spanContext.TraceID == zeroTraceID; got != c.ZeroTraceID {
		want := "zero"
		if !c.ZeroTraceID {
			want = "non-" + want
		}
		t.Errorf("got TraceID = %x, want %s", spanContext.TraceID, want)
	}
	if got := spanContext.SpanID == zeroSpanID; got != c.ZeroSpanID {
		want := "zero"
		if !c.ZeroSpanID {
			want = "non-" + want
		}
		t.Errorf("got SpanID = %x, want %s", spanContext.SpanID, want)
	}
	if got, want := spanContext.Sampled, c.Sampled; got != want {
		t.Errorf("got Sampled = %v, want %v", got, want)
	}

	if got, want := len(span.spanRecorder().spans), c.RecorderLen; got != want {
		t.Errorf("got %d spans in recorder, want %d", got, want)
	}
}

func TestToTraceparent(t *testing.T) {
	tests := []struct {
		ctx  SpanContext
		want string
	}{
		{SpanContext{}, "00000000000000000000000000000000-0000000000000000-0"},
		{SpanContext{Sampled: true}, "00000000000000000000000000000000-0000000000000000-1"},
		{SpanContext{TraceID: TraceID{1}}, "01000000000000000000000000000000-0000000000000000-0"},
		{SpanContext{SpanID: SpanID{1}}, "00000000000000000000000000000000-0100000000000000-0"},
	}
	for _, tt := range tests {
		if got := tt.ctx.ToTraceparent(); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
	}
}
