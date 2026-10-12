package sentry

import (
	"context"
	"sync"

	"github.com/getsentry/sentry-go/internal/contextkey"
)

type scopeContextKey struct{}
type clientContextKey = contextkey.Client

// globalScope is the process-wide global scope.
var globalScope = NewScope()

var (
	globalClientMu sync.RWMutex
	globalClient   *Client
)

// GlobalScope returns the process-wide global scope.
func GlobalScope() *Scope {
	return globalScope
}

// ScopeFromContext returns the scope carried by ctx, or nil when there is none.
func ScopeFromContext(ctx context.Context) *Scope {
	if ctx == nil {
		return nil
	}
	scope, _ := ctx.Value(scopeContextKey{}).(*Scope)
	return scope
}

func scopeAndTraceFallback(ctx, fallback context.Context) (*Scope, context.Context) {
	if scope := ScopeFromContext(ctx); scope != nil {
		return scope, nil
	}
	return ScopeFromContext(fallback), fallback
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
		scope.span = nil
		if span := SpanFromContext(ctx); span != nil {
			scope.span = span.GetTransaction()
		}
	} else {
		scope = parent.Clone()
	}
	return ContextWithScope(ctx, scope), scope
}

// ClientFromContext returns the client attached to ctx, or the global client.
func ClientFromContext(ctx context.Context) *Client {
	if ctx != nil {
		if client, ok := ctx.Value(clientContextKey{}).(*Client); ok {
			return client
		}
	}
	return normalizeClient(globalClientSnapshot())
}

// clientFromContexts retains the construction client unless emission binds one.
func clientFromContexts(ctx, fallback context.Context) *Client {
	if ctx != nil {
		if client, ok := ctx.Value(clientContextKey{}).(*Client); ok {
			return client
		}
	}
	return ClientFromContext(fallback)
}

func scopeFromContextOrGlobal(ctx context.Context) *Scope {
	if scope := ScopeFromContext(ctx); scope != nil {
		return scope
	}
	return GlobalScope()
}

func setGlobalClient(client *Client) {
	globalClientMu.Lock()
	globalClient = client
	globalClientMu.Unlock()
}

func globalClientSnapshot() *Client {
	globalClientMu.RLock()
	client := globalClient
	globalClientMu.RUnlock()
	return client
}
