package sentry

import (
	"context"
	"testing"
)

func TestScopeFromContext(t *testing.T) {
	if scope := ScopeFromContext(context.Background()); scope != nil {
		t.Fatalf("ScopeFromContext returned %p for an unscoped context", scope)
	}

	ctx, scope := WithIsolationScope(context.Background())
	if got := ScopeFromContext(ctx); got != scope {
		t.Fatalf("ScopeFromContext returned %p, want %p", got, scope)
	}
}

func TestContextWithScope(t *testing.T) {
	type contextKey struct{}

	parent := context.WithValue(context.Background(), contextKey{}, "value")
	scope := NewScope()
	ctx := ContextWithScope(parent, scope)

	if got := ScopeFromContext(ctx); got != scope {
		t.Fatalf("ScopeFromContext returned %p, want %p", got, scope)
	}
	if got := ctx.Value(contextKey{}); got != "value" {
		t.Fatalf("parent context value = %v, want value", got)
	}
}

func TestContextWithScopeNilScopeReturnsOriginalContext(t *testing.T) {
	ctx := context.Background()
	if got := ContextWithScope(ctx, nil); got != ctx {
		t.Fatal("nil scope did not return the original context")
	}
}

func TestIsolationScopeSharesDownstreamMutations(t *testing.T) {
	type contextKey struct{}

	ctx, scope := WithIsolationScope(context.Background())
	child := context.WithValue(ctx, contextKey{}, "value")
	childScope := ScopeFromContext(child)
	childScope.SetUser(User{ID: "123"})

	if scope.user.ID != "123" {
		t.Fatal("downstream mutation was not visible to the boundary owner")
	}
}

func TestWithIsolationScopeCreatesIndependentBoundaries(t *testing.T) {
	parentCtx, parent := WithIsolationScope(context.Background())
	parent.SetTag("inherited", "yes")
	parent.SetPropagationContext(PropagationContext{
		TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		SpanID:  SpanIDFromHex("bbbbbbbbbbbbbbbb"),
		DynamicSamplingContext: DynamicSamplingContext{
			Entries: map[string]string{"release": "parent"},
			Frozen:  true,
		},
	})
	parentPropagation := parent.propagationContextSnapshot()

	firstCtx, first := WithIsolationScope(parentCtx)
	secondCtx, second := WithIsolationScope(parentCtx)
	first.SetTag("worker", "first")
	second.SetTag("worker", "second")

	if ScopeFromContext(firstCtx) != first || ScopeFromContext(secondCtx) != second {
		t.Fatal("derived contexts do not carry their returned isolation scopes")
	}
	if first == parent || second == parent || first == second {
		t.Fatal("isolation boundaries alias")
	}
	if first.tags["inherited"] != "yes" || second.tags["inherited"] != "yes" {
		t.Fatal("isolation boundaries did not inherit parent enrichment")
	}
	if _, ok := parent.tags["worker"]; ok {
		t.Fatal("child mutation leaked into parent")
	}
	if first.tags["worker"] != "first" || second.tags["worker"] != "second" {
		t.Fatal("sibling isolation mutations leaked")
	}
	if first.propagationContext.TraceID != parentPropagation.TraceID ||
		second.propagationContext.TraceID != parentPropagation.TraceID {
		t.Fatal("scope isolation changed the propagation trace ID")
	}
	firstPropagation := first.propagationContextSnapshot()
	firstPropagation.DynamicSamplingContext.Entries["release"] = "first"
	first.SetPropagationContext(firstPropagation)
	if got := parent.propagationContextSnapshot().DynamicSamplingContext.Entries["release"]; got != "parent" {
		t.Fatalf("child propagation mutation leaked into parent: %q", got)
	}
	if got := second.propagationContextSnapshot().DynamicSamplingContext.Entries["release"]; got != "parent" {
		t.Fatalf("child propagation mutation leaked into sibling: %q", got)
	}
}

func TestWithIsolationScopeDoesNotCloneGlobalScope(t *testing.T) {
	global := GlobalScope()
	global.SetTag("global-only", "yes")
	t.Cleanup(func() { global.RemoveTag("global-only") })

	ctx, scope := WithIsolationScope(context.Background())
	if ScopeFromContext(ctx) != scope {
		t.Fatal("derived context does not carry the returned scope")
	}
	if scope == global {
		t.Fatal("isolation scope aliases global scope")
	}
	if _, ok := scope.tags["global-only"]; ok {
		t.Fatal("isolation scope cloned global data")
	}
}

func TestWithScopeContextCreatesTemporaryFork(t *testing.T) {
	ctx, parent := WithIsolationScope(context.Background())
	parent.SetTag("parent", "yes")
	propagation := parent.propagationContextSnapshot()

	WithScopeContext(ctx, func(childCtx context.Context, child *Scope) {
		if ScopeFromContext(childCtx) != child {
			t.Fatal("callback context does not carry callback scope")
		}
		if child == parent || child.tags["parent"] != "yes" {
			t.Fatal("callback scope did not inherit an independent enrichment copy")
		}
		if child.propagationContext.TraceID != propagation.TraceID {
			t.Fatal("temporary scope fork did not continue propagation")
		}
		child.SetTag("temporary", "yes")
	})

	if _, ok := parent.tags["temporary"]; ok {
		t.Fatal("temporary mutation leaked into parent")
	}
}

func TestScopeClientResolution(t *testing.T) {
	globalClient, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	operationClient, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	otherGlobalClient, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	global := GlobalScope()
	previousGlobal := global.clientOverrideSnapshot()
	global.SetClient(globalClient)
	t.Cleanup(func() { global.SetClient(previousGlobal) })

	standalone := NewScope()
	if standalone.clientOverrideSnapshot() != nil {
		t.Fatal("NewScope has an unexpected client override")
	}
	if standalone.Client() != globalClient {
		t.Fatal("standalone scope did not resolve the global client")
	}

	ctx, scope := WithIsolationScope(context.Background())
	if GetClient(ctx) != globalClient || scope.Client() != globalClient || scope.clientOverrideSnapshot() != nil {
		t.Fatal("new operation scope did not inherit the global client dynamically")
	}

	global.SetClient(otherGlobalClient)
	if GetClient(ctx) != otherGlobalClient || scope.Client() != otherGlobalClient {
		t.Fatal("existing operation scope did not observe the updated global client")
	}

	scope.SetClient(NewNoopClient())
	if GetClient(ctx) != otherGlobalClient || scope.Client() != otherGlobalClient {
		t.Fatal("disabled operation client did not fall back to the enabled global client")
	}

	scope.SetClient(operationClient)
	if GetClient(ctx) != operationClient || scope.Client() != operationClient {
		t.Fatal("enabled operation client did not override the global client")
	}

	childCtx, child := WithIsolationScope(ctx)
	if GetClient(childCtx) != operationClient {
		t.Fatal("child isolation did not inherit the explicit client override")
	}
	child.SetClient(nil)
	if GetClient(childCtx) != otherGlobalClient || GetClient(ctx) != operationClient {
		t.Fatal("clearing the child override did not fall back to global independently")
	}
}

func TestWithScopeContextLastEventIDIsLocal(t *testing.T) {
	ctx, operation := WithIsolationScope(context.Background())
	id := EventID("0123456789abcdef0123456789abcdef")

	WithScopeContext(ctx, func(_ context.Context, fork *Scope) {
		fork.setLastEventID(id)
		if got := fork.LastEventID(); got != id {
			t.Fatalf("fork LastEventID = %q, want %q", got, id)
		}
	})

	if got := operation.LastEventID(); got != "" {
		t.Fatalf("temporary fork update leaked to operation scope: %q", got)
	}
}

func TestLastEventIDIsIndependentPerScope(t *testing.T) {
	ctx, operation := WithIsolationScope(context.Background())
	id := EventID("0123456789abcdef0123456789abcdef")

	clone := operation.Clone()
	clone.setLastEventID(id)
	if got := operation.LastEventID(); got != "" {
		t.Fatalf("clone update leaked to operation scope: %q", got)
	}

	_, child := WithIsolationScope(ctx)
	child.setLastEventID(id)
	if got := operation.LastEventID(); got != "" {
		t.Fatalf("isolation update leaked to operation scope: %q", got)
	}
}

func TestScopeLastEventIDSurvivesCloneAndClear(t *testing.T) {
	scope := NewScope()
	id := EventID("0123456789abcdef0123456789abcdef")
	scope.setLastEventID(id)

	clone := scope.Clone()
	scope.Clear()

	if got := scope.LastEventID(); got != id {
		t.Fatalf("LastEventID after Clear = %q, want %q", got, id)
	}
	if got := clone.LastEventID(); got != id {
		t.Fatalf("clone LastEventID = %q, want %q", got, id)
	}
}
