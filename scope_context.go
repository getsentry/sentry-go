package sentry

import "context"

type scopeContextKey struct{}

// globalScope is the process-wide global scope.
var globalScope = newScopeWithClient(NewNoopClient())

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

func contextWithScope(ctx context.Context, scope *Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// WithIsolationScope returns a derived context and an independent scope.
// It clones a carried scope or creates an empty scope when ctx does not carry
// one. Trace state carried by ctx is preserved.
func WithIsolationScope(ctx context.Context) (context.Context, *Scope) {
	parent := ScopeFromContext(ctx)
	var scope *Scope
	if parent == nil {
		scope = NewScope()
	} else {
		scope = parent.Clone()
	}
	return contextWithScope(ctx, scope), scope
}

// WithScopeContext invokes fn with a derived context carrying a cloned scope.
func WithScopeContext(ctx context.Context, fn func(context.Context, *Scope)) { // TODO: should remove WithScope when hub is removed.
	if fn == nil {
		return
	}

	parent := ScopeFromContext(ctx)
	if parent == nil {
		parent = NewScope()
	}
	scope := parent.Clone()
	fn(contextWithScope(ctx, scope), scope)
}

// GetClient returns the first enabled client for ctx.
func GetClient(ctx context.Context) *Client {
	if scope := ScopeFromContext(ctx); scope != nil {
		return scope.Client()
	}
	return GlobalScope().Client()
}
