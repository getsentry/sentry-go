package sentry

import "context"

type scopeContextKey struct{}

// globalScope is the process-wide global scope.
var globalScope = newScopeWithClient(NewNoopClient())

// GlobalScope returns the process-wide global scope.
func GlobalScope() *Scope {
	return globalScope
}

func scopeFromContext(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(scopeContextKey{}).(*Scope)
	return scope
}

// ScopeFromContext returns the isolation scope carried by ctx. If ctx does not carry one,
// it creates an isolation scope and returns a derived context carrying that scope.
//
// If ctx already carries an isolation scope, ScopeFromContext returns the exact
// input context and the existing scope.
func ScopeFromContext(ctx context.Context) (context.Context, *Scope) {
	if scope := scopeFromContext(ctx); scope != nil {
		return ctx, scope
	}

	scope := newIsolationScope()
	return context.WithValue(ctx, scopeContextKey{}, scope), scope
}

// WithIsolation returns an independent isolation context. It clones a scope already
// carried by ctx or creates a fresh one if none exist.
func WithIsolation(ctx context.Context) context.Context {
	scope := scopeFromContext(ctx)
	if scope == nil {
		scope = newIsolationScope()
	} else {
		scope = scope.Clone()
	}
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// newIsolationScope creates a new operation scope bound to the client currently
// set on GlobalScope. The client interface value is copied; the Client object is
// shared and the Scope itself is not. This guarantees that every context-carried
// operation scope has a non-nil client without treating a no-op client as an
// “unbound” sentinel.
//
// Use this only when there is no parent Scope to clone. A nested isolation
// boundary must clone its parent Scope instead so an explicit client binding is
// preserved.
func newIsolationScope() *Scope {
	return newScopeWithClient(GlobalScope().client())
}

// GetClient returns the effective non-nil client for ctx. A carried operation
// Scope always has a client binding; when ctx has no Scope, the current global
// client is used. Rebinding GlobalScope affects only contexts without an
// already-created operation Scope.
func GetClient(ctx context.Context) Client {
	if scope := scopeFromContext(ctx); scope != nil {
		return normalizeClient(scope.client())
	}
	return normalizeClient(GlobalScope().client())
}
