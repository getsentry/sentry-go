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

	assert.NotNil(t, f.Hub, "hub should not be nil")
	assert.NotNil(t, f.Client, "client should not be nil")
	assert.NotNil(t, f.Transport, "transport should not be nil")
	assert.NotSame(t, sentry.CurrentHub(), f.Hub, "isolated fixture hub should not be the global hub")
}

func TestNewSentryFixture_Global(t *testing.T) {
	f := NewFixture(t, WithGlobal())

	assert.Same(t, sentry.CurrentHub(), f.Hub, "global fixture hub should be the current hub")
	assert.NotNil(t, f.Client, "client should not be nil")
}

func TestNewSentryFixture_WithClientOptions_Tracing(t *testing.T) {
	t.Parallel()

	f := NewFixture(t, WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))

	span := sentry.StartTransaction(
		sentry.SetHubOnContext(t.Context(), f.Hub),
		"test-transaction",
	)
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

	f.Hub.CaptureMessage("hello")
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
	assert.Same(t, f.Hub, sentry.GetHubFromContext(ctx), "context hub")
}

func TestNewContext(t *testing.T) {
	t.Parallel()

	parent := context.WithValue(context.Background(), contextKey{}, "value")
	ctx := NewContext(t, parent)

	assert.Equal(t, "value", ctx.Value(contextKey{}), "context value")
	assert.NotNil(t, sentry.GetHubFromContext(ctx), "context hub")
}

func TestNewContext_NilParent(t *testing.T) {
	t.Parallel()

	ctx := NewContext(t, nil)

	assert.NotNil(t, sentry.GetHubFromContext(ctx), "context hub")
	assert.Nil(t, ctx.Value(contextKey{}), "context value")
}

func TestSentryFixture_Events_IncludesTransactions(t *testing.T) {
	t.Parallel()

	f := NewFixture(t, WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))
	ctx := sentry.SetHubOnContext(t.Context(), f.Hub)
	f.Hub.CaptureMessage("error event")
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
	f.Hub.CaptureMessage("one")
	f.Hub.CaptureMessage("two")

	f.AssertEventCount(2)
}

func TestSentryFixture_DiffEvents(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	f.Hub.CaptureMessage("hello")

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
	f.Hub.CaptureMessage("hello")

	want := []*sentry.Event{
		{Message: "hello"},
	}
	if diff := f.DiffEvents(want, cmpopts.IgnoreFields(sentry.Event{}, "Level")); diff != "" {
		t.Errorf("DiffEvents mismatch (-want +got):\n%s", diff)
	}
}

func TestSentryFixture_AssertHubIsolation_Pass(t *testing.T) {
	t.Parallel()

	f := NewFixture(t)
	requestHub := f.Hub.Clone()

	f.AssertHubIsolation(requestHub)
}

func TestSentryFixture_AssertHubIsolation_DetectsNil(t *testing.T) {
	t.Parallel()

	mock := &testing.T{}
	f := &Fixture{T: mock, Hub: sentry.CurrentHub().Clone()}
	f.AssertHubIsolation(nil)

	assert.True(t, mock.Failed(), "AssertHubIsolation should fail when requestHub is nil")
}

func TestSentryFixture_AssertHubIsolation_DetectsSameHub(t *testing.T) {
	t.Parallel()

	mock := &testing.T{}
	f := &Fixture{T: mock, Hub: sentry.CurrentHub().Clone()}

	f.AssertHubIsolation(f.Hub) // same pointer

	assert.True(t, mock.Failed(), "AssertHubIsolation should fail when requestHub is the same pointer")
}

func TestSentryFixture_AssertHubIsolation_DetectsScopeLeakage(t *testing.T) {
	t.Parallel()

	// Create a "bad clone" that shares the scope pointer.
	mock := &testing.T{}
	f := &Fixture{T: mock, Hub: sentry.CurrentHub().Clone()}
	badHub := sentry.NewHub(f.Hub.Client(), f.Hub.Scope())

	f.AssertHubIsolation(badHub)

	assert.True(t, mock.Failed(), "AssertHubIsolation should fail when scopes are shared (not cloned)")
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
