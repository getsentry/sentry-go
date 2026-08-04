package sentry

import (
	"context"
	"testing"
)

func TestScopeFromContext(t *testing.T) {
	parent := context.Background()
	ctx, scope := ScopeFromContext(parent)
	if ctx == parent || scope == GlobalScope() {
		t.Fatal("scope miss did not create an independent scoped context")
	}

	sameCtx, sameScope := ScopeFromContext(ctx)
	if sameCtx != ctx || sameScope != scope {
		t.Fatal("scope hit did not return the exact context and scope")
	}

	otherCtx, otherScope := ScopeFromContext(parent)
	if otherCtx == ctx || otherScope == scope {
		t.Fatal("independent misses from an unscoped context aliased")
	}
}

func TestIsolationScopeSharesDownstreamMutations(t *testing.T) {
	type contextKey struct{}

	ctx, scope := ScopeFromContext(context.Background())
	child := context.WithValue(ctx, contextKey{}, "value")
	_, childScope := ScopeFromContext(child)
	childScope.SetUser(User{ID: "123"})

	if scope.user.ID != "123" {
		t.Fatal("downstream mutation was not visible to the boundary owner")
	}
}

func TestWithIsolationCreatesIndependentBoundaries(t *testing.T) {
	parentCtx, parent := ScopeFromContext(context.Background())
	parent.SetTag("inherited", "yes")

	firstCtx := WithIsolation(parentCtx)
	secondCtx := WithIsolation(parentCtx)
	_, first := ScopeFromContext(firstCtx)
	_, second := ScopeFromContext(secondCtx)
	first.SetTag("worker", "first")
	second.SetTag("worker", "second")

	if first == parent || second == parent || first == second {
		t.Fatal("isolation boundaries alias")
	}
	if first.tags["inherited"] != "yes" || second.tags["inherited"] != "yes" {
		t.Fatal("isolation boundaries did not inherit parent data")
	}
	if _, ok := parent.tags["worker"]; ok {
		t.Fatal("child mutation leaked into parent")
	}
	if first.tags["worker"] != "first" || second.tags["worker"] != "second" {
		t.Fatal("sibling isolation mutations leaked")
	}
}

func TestWithIsolationDoesNotCloneGlobalScope(t *testing.T) {
	global := GlobalScope()
	global.SetTag("global-only", "yes")
	t.Cleanup(func() { global.RemoveTag("global-only") })

	ctx := WithIsolation(context.Background())
	_, scope := ScopeFromContext(ctx)
	if scope == global {
		t.Fatal("isolation scope aliases global scope")
	}
	if _, ok := scope.tags["global-only"]; ok {
		t.Fatal("isolation scope cloned global data")
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
	childClient, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}

	global := GlobalScope()
	previousGlobal := global.client()
	global.SetClient(globalClient)
	t.Cleanup(func() { global.SetClient(previousGlobal) })

	standalone := NewScope()
	if standalone.client().IsEnabled() {
		t.Fatal("NewScope was not bound to a no-op client")
	}

	ctx, scope := ScopeFromContext(context.Background())
	if GetClient(ctx) != globalClient || scope.client() != globalClient {
		t.Fatal("new isolation did not snapshot the global client")
	}

	global.SetClient(childClient)
	if GetClient(ctx) != globalClient || standalone.client().IsEnabled() {
		t.Fatal("existing scopes followed a later global client change")
	}
	global.SetClient(globalClient)

	scope.SetClient(operationClient)

	childCtx := WithIsolation(ctx)
	_, child := ScopeFromContext(childCtx)
	if GetClient(childCtx) != operationClient {
		t.Fatal("child scope did not inherit client reference")
	}
	child.SetClient(childClient)
	if GetClient(childCtx) != childClient || GetClient(ctx) != operationClient {
		t.Fatal("child client binding was not independent")
	}
}
