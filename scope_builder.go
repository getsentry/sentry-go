package sentry

import (
	"context"
	"net/http"

	"github.com/getsentry/sentry-go/attribute"
)

// ScopeBuilder provides a batching API for applying multiple scope mutations
// to a derived context without repeated ctx reassignment.
type ScopeBuilder struct {
	ctx context.Context
}

func (b *ScopeBuilder) SetUser(user User) {
	b.ctx = SetUser(b.ctx, user)
}

func (b *ScopeBuilder) SetRequest(r *http.Request) {
	b.ctx = SetRequest(b.ctx, r)
}

func (b *ScopeBuilder) SetRequestBody(body []byte) {
	b.ctx = SetRequestBody(b.ctx, body)
}

func (b *ScopeBuilder) SetTag(key, value string) {
	b.ctx = SetTag(b.ctx, key, value)
}

func (b *ScopeBuilder) SetTags(tags map[string]string) {
	b.ctx = SetTags(b.ctx, tags)
}

func (b *ScopeBuilder) SetContext(key string, value Context) {
	b.ctx = SetContext(b.ctx, key, value)
}

func (b *ScopeBuilder) SetContexts(contexts map[string]Context) {
	b.ctx = SetContexts(b.ctx, contexts)
}

func (b *ScopeBuilder) SetLevel(level Level) {
	b.ctx = SetLevel(b.ctx, level)
}

func (b *ScopeBuilder) SetFingerprint(fingerprint []string) {
	b.ctx = SetFingerprint(b.ctx, fingerprint)
}

func (b *ScopeBuilder) SetAttributes(attrs ...attribute.Builder) {
	b.ctx = SetAttributes(b.ctx, attrs...)
}

func (b *ScopeBuilder) RemoveAttribute(key string) {
	b.ctx = RemoveAttribute(b.ctx, key)
}

func (b *ScopeBuilder) AddBreadcrumb(breadcrumb *Breadcrumb) {
	b.ctx = AddBreadcrumb(b.ctx, breadcrumb)
}

func (b *ScopeBuilder) AddEventProcessor(processor EventProcessor) {
	b.ctx = AddEventProcessor(b.ctx, processor)
}
