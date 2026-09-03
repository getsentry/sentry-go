package sentry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextWithScope(t *testing.T) {
	ctx := context.Background()
	scope := NewScope()

	assert.Equal(t, ctx, ContextWithScope(ctx, nil))
	assert.Nil(t, ScopeFromContext(ctx))
	assert.Same(t, scope, ScopeFromContext(ContextWithScope(ctx, scope)))
}

func TestContextWithClient(t *testing.T) {
	globalClient, err := NewClient(ClientOptions{})
	require.NoError(t, err)
	parentClient, err := NewClient(ClientOptions{})
	require.NoError(t, err)
	childClient, err := NewClient(ClientOptions{})
	require.NoError(t, err)
	t.Cleanup(globalClient.Close)
	t.Cleanup(parentClient.Close)
	t.Cleanup(childClient.Close)

	previousGlobalClient := globalClientSnapshot()
	setGlobalClient(globalClient)
	t.Cleanup(func() { setGlobalClient(previousGlobalClient) })

	parentScope := NewScope()
	parentScope.SetTag("inherited", "value")
	parentCtx := ContextWithClient(ContextWithScope(context.Background(), parentScope), parentClient)

	childCtx := ContextWithClient(parentCtx, childClient)
	childScope := ScopeFromContext(childCtx)

	assert.Same(t, parentScope, childScope)
	assert.Same(t, parentClient, ClientFromContext(parentCtx))
	assert.Same(t, childClient, ClientFromContext(childCtx))
	assert.Equal(t, "value", childScope.tags["inherited"])
	assert.Nil(t, ScopeFromContext(ContextWithClient(context.Background(), childClient)))

	inheritedCtx := ContextWithClient(childCtx, nil)
	assert.Equal(t, childCtx, inheritedCtx)
	assert.Same(t, childClient, ClientFromContext(inheritedCtx))
}

func TestGlobalScopeHasNoPropagationContext(t *testing.T) {
	global := newGlobalScope()
	propagation := global.propagationContextSnapshot()

	assert.Equal(t, zeroTraceID, propagation.TraceID)
	assert.Equal(t, zeroSpanID, propagation.SpanID)
	event := global.ApplyToEvent(NewEvent(), nil, NewNoopClient())
	assert.NotContains(t, event.Contexts, "trace")

	global.Clear()
	assert.Equal(t, propagation, global.propagationContextSnapshot())
}

func TestWithIsolationScope(t *testing.T) {
	global := GlobalScope()
	global.SetTag("global-only", "value")
	t.Cleanup(func() { global.RemoveTag("global-only") })

	parent := NewScope()
	parent.SetTag("inherited", "value")
	parent.SetPropagationContext(PropagationContext{
		TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"release": "parent"},
		},
	})
	parentCtx := ContextWithScope(context.Background(), parent)

	childCtx, child := WithIsolationScope(parentCtx)
	child.SetTag("child", "value")
	childPropagation := child.propagationContextSnapshot()
	childPropagation.DynamicSamplingContext.Entries["release"] = "child"
	child.SetPropagationContext(childPropagation)

	assert.Same(t, child, ScopeFromContext(childCtx))
	assert.NotSame(t, parent, child)
	assert.Equal(t, "value", child.tags["inherited"])
	assert.NotContains(t, parent.tags, "child")
	assert.Equal(t, "parent", parent.propagationContextSnapshot().DynamicSamplingContext.Entries["release"])

	rootCtx, root := WithIsolationScope(context.Background())
	assert.Same(t, root, ScopeFromContext(rootCtx))
	assert.NotSame(t, global, root)
	assert.Equal(t, "value", root.tags["global-only"])
	global.SetTag("after-snapshot", "value")
	t.Cleanup(func() { global.RemoveTag("after-snapshot") })
	assert.NotContains(t, root.tags, "after-snapshot")
}

func TestClientFromContext(t *testing.T) {
	globalClient, err := NewClient(ClientOptions{})
	require.NoError(t, err)
	localClient, err := NewClient(ClientOptions{})
	require.NoError(t, err)
	noopClient := NewNoopClient()

	previous := globalClientSnapshot()
	setGlobalClient(globalClient)
	t.Cleanup(func() { setGlobalClient(previous) })

	tests := []struct {
		name string
		ctx  context.Context
		want *Client
	}{
		{name: "no scope", ctx: context.Background(), want: globalClient},
		{name: "nil client leaves context unchanged", ctx: ContextWithClient(context.Background(), nil), want: globalClient},
		{name: "local override", ctx: ContextWithClient(context.Background(), localClient), want: localClient},
		{name: "explicit noop", ctx: ContextWithClient(context.Background(), noopClient), want: noopClient},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Same(t, test.want, ClientFromContext(test.ctx))
		})
	}

	parentCtx := ContextWithClient(context.Background(), localClient)
	assert.Equal(t, parentCtx, ContextWithClient(parentCtx, nil))
	assert.Same(t, localClient, ClientFromContext(ContextWithClient(parentCtx, nil)))
	setGlobalClient(nil)
	assert.False(t, ClientFromContext(context.Background()).IsEnabled())
}

func TestScopeLastEventIDIsNotClonedAndSurvivesClear(t *testing.T) {
	scope := NewScope()
	id := EventID("0123456789abcdef0123456789abcdef")
	scope.setLastEventID(id)

	clone := scope.Clone()
	scope.Clear()

	assert.Equal(t, id, scope.lastEventIDSnapshot())
	assert.Empty(t, clone.lastEventIDSnapshot())
}

func globalClientSnapshot() *Client {
	return globalClient.Load()
}

func TestLastEventIDWithoutContextScopeUsesGlobalScope(t *testing.T) {
	global := GlobalScope()
	previousID := global.lastEventIDSnapshot()
	t.Cleanup(func() { global.setLastEventID(previousID) })

	id := EventID("0123456789abcdef0123456789abcdef")
	var scope *Scope
	scope.setLastEventID(id)

	assert.Equal(t, id, scope.lastEventIDSnapshot())
	assert.Equal(t, id, LastEventID(context.Background()))
}
