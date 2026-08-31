package sentrytest

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
)

type contextKey struct{}

func TestNewSentryFixture_Isolated(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)

	assert.NotNil(t, f.Scope, "scope should not be nil")
	assert.NotNil(t, f.Context, "context should not be nil")
	assert.NotNil(t, f.Client, "client should not be nil")
	assert.NotNil(t, f.Transport, "transport should not be nil")
	assert.NotSame(t, sentry.GlobalScope(), f.Scope, "isolated fixture scope should not be the global scope")
}

func TestNewSentryFixture_Global(t *testing.T) {
	f := NewFixture(t, WithGlobal())

	assert.Same(t, sentry.GlobalScope().Client(), f.Client, "global fixture client should be globally configured")
	assert.NotNil(t, f.Client, "client should not be nil")
}

func TestNewSentryFixture_WithClientOptions_Tracing(t *testing.T) {
	t.Parallel()

	f := NewFixture(t, WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))

	span := sentry.StartTransaction(f.NewContext(t.Context()), "test-transaction")
	span.Finish()
	f.Flush()

	events := f.Events()
	assert.Len(t, events, 1, "event count")
	assert.Equal(t, "transaction", events[0].Type, "event type")
}

func TestNewSentryFixture_WithClientOptions(t *testing.T) {
	t.Parallel()

	f := NewFixture(t,
		WithClientOptions(sentry.ClientOptions{
			Environment: "test-env",
		}),
	)

	sentry.CaptureMessage(f.Context, "hello")
	f.Flush()

	events := f.Events()
	assert.Len(t, events, 1, "event count")
	assert.Equal(t, "test-env", events[0].Environment, "environment")
}

func TestFixture_NewContext(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	parent := context.WithValue(context.Background(), contextKey{}, "value")

	ctx := f.NewContext(parent)

	assert.Equal(t, "value", ctx.Value(contextKey{}), "context value")
	assert.NotNil(t, sentry.ScopeFromContext(ctx), "context scope")
	assert.Same(t, f.Client, sentry.GetClient(ctx), "context client")
}

func TestNewContext(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(context.Background(), contextKey{}, "value")
	ctx := NewContext(parent, t)

	assert.Equal(t, "value", ctx.Value(contextKey{}), "context value")
	assert.NotNil(t, sentry.ScopeFromContext(ctx), "context scope")
}

func TestNewContext_NilParent(t *testing.T) {
	t.Parallel()

	ctx := NewContext(nil, t) // nolint: staticcheck // SA1012: passing nil context for the test

	assert.NotNil(t, sentry.ScopeFromContext(ctx), "context scope")
	assert.Nil(t, ctx.Value(contextKey{}), "context value")
}

func TestSentryFixture_Events_IncludesTransactions(t *testing.T) {
	t.Parallel()

	f := NewFixture(t, WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))
	ctx := f.NewContext(t.Context())
	sentry.CaptureMessage(ctx, "error event")
	span := sentry.StartTransaction(ctx, "test-tx")
	span.Finish()

	f.Flush()

	events := f.Events()
	assert.Len(t, events, 2, "event count")
	assert.Equal(t, "error event", events[0].Message, "event message")
	assert.Equal(t, "transaction", events[1].Type, "event type")
}

func TestSentryFixture_AssertEventCount(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	sentry.CaptureMessage(f.Context, "one")
	sentry.CaptureMessage(f.Context, "two")

	f.AssertEventCount(2)
}

func TestSentryFixture_DiffEvents(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	sentry.CaptureMessage(f.Context, "hello")

	want := []*sentry.Event{
		{Message: "hello", Level: sentry.LevelInfo},
	}
	if diff := f.DiffEvents(want); diff != "" {
		t.Errorf("DiffEvents mismatch (-want +got):\n%s", diff)
	}
}

func TestSentryFixture_DiffEvents_WithExtraOpts(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	sentry.CaptureMessage(f.Context, "hello")

	want := []*sentry.Event{
		{Message: "hello"},
	}
	if diff := f.DiffEvents(want, cmpopts.IgnoreFields(sentry.Event{}, "Level")); diff != "" {
		t.Errorf("DiffEvents mismatch (-want +got):\n%s", diff)
	}
}

func TestSentryFixture_AssertScopeIsolation_Pass(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	requestScope := f.Scope.Clone()

	f.AssertScopeIsolation(requestScope)
}

func TestSentryFixture_AssertScopeIsolation_DetectsNil(t *testing.T) {
	t.Parallel()

	mock := &testing.T{}
	f := &Fixture{T: mock, Scope: sentry.GlobalScope().Clone()}
	f.AssertScopeIsolation(nil)

	assert.True(t, mock.Failed(), "AssertScopeIsolation should fail when requestScope is nil")
}

func TestSentryFixture_AssertScopeIsolation_DetectsSameScope(t *testing.T) {
	t.Parallel()

	mock := &testing.T{}
	f := &Fixture{T: mock, Scope: sentry.GlobalScope().Clone()}

	f.AssertScopeIsolation(f.Scope)

	assert.True(t, mock.Failed(), "AssertScopeIsolation should fail when requestScope is the same pointer")
}

func TestDefaultEventCmpOpts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    *sentry.Event
		b    *sentry.Event
	}{
		{
			name: "ignores variable event fields",
			a: &sentry.Event{
				Message: "same",
				EventID: "aaa",
			},
			b: &sentry.Event{
				Message: "same",
				EventID: "bbb",
			},
		},
		{
			name: "ignores request env and equates empty collections",
			a: &sentry.Event{
				Message: "same",
				Request: &sentry.Request{Env: map[string]string{"A": "1"}},
				Tags:    nil,
			},
			b: &sentry.Event{
				Message: "same",
				Request: &sentry.Request{},
				Tags:    map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if diff := cmp.Diff(tt.a, tt.b, DefaultEventCmpOpts...); diff != "" {
				t.Errorf("DefaultEventCmpOpts mismatch (-a +b):\n%s", diff)
			}
		})
	}
}
