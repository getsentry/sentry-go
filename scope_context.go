package sentry

import (
	"context"
	"sync/atomic"
)

type scopeContextKey struct{}
type clientContextKey struct{}
type requestContextKey struct{}

// RequestContextKey is the key used to store the current request object in a
// capture context.
var RequestContextKey requestContextKey

// globalScope is the process-wide global scope.
// It intentionally has no propagation context: trace state belongs to an
// isolation scope carried by context.Context.
var globalScope = newGlobalScope()
var globalClient atomic.Pointer[Client]

func newGlobalScope() *Scope {
	return &Scope{scopeData: newScopeDataWithPropagation(PropagationContext{})}
}

// GlobalScope returns the process-wide global scope.
func GlobalScope() *Scope {
	return globalScope
}

// ScopeFromContext returns the scope carried by ctx, or nil when ctx
// does not carry one.
func ScopeFromContext(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(scopeContextKey{}).(*Scope)
	return scope
}

// ContextWithScope returns a context carrying scope. The supplied scope is the
// complete scope for the derived context. A nil scope leaves ctx unchanged.
func ContextWithScope(ctx context.Context, scope *Scope) context.Context {
	if scope == nil {
		return ctx
	}
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// ContextWithClient returns a derived context that routes telemetry through
// client without changing its scope. A nil client leaves ctx unchanged.
func ContextWithClient(ctx context.Context, client *Client) context.Context {
	if client == nil {
		return ctx
	}
	return context.WithValue(ctx, clientContextKey{}, client)
}

// WithIsolationScope returns a derived context and an independent scope.
// It clones a carried scope, or snapshots the global scope when ctx does not
// carry one. A root isolation scope starts a new propagation context.
func WithIsolationScope(ctx context.Context) (context.Context, *Scope) {
	parent := ScopeFromContext(ctx)
	var scope *Scope
	if parent == nil {
		scope = GlobalScope().Clone()
		scope.propagationContext = NewPropagationContext()
	} else {
		scope = parent.Clone()
	}
	return ContextWithScope(ctx, scope), scope
}

// ClientFromContext returns the client attached to ctx, or the global client.
func ClientFromContext(ctx context.Context) *Client {
	return clientFromContexts(ctx)
}

// clientFromContexts returns the first explicitly bound client, falling back
// to the global client when none of the contexts carries one.
func clientFromContexts(ctxs ...context.Context) *Client {
	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
		if client, ok := ctx.Value(clientContextKey{}).(*Client); ok {
			return client
		}
	}
	return normalizeClient(globalClient.Load())
}

func scopeFromContextOrGlobal(ctx context.Context) *Scope {
	if scope := ScopeFromContext(ctx); scope != nil {
		return scope
	}
	return GlobalScope()
}

func setGlobalClient(client *Client) {
	globalClient.Store(normalizeClient(client))
}
