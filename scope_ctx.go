package sentry

import (
	"bytes"
	"context"
	"io"
	"maps"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// globalScope is the default scope used without ctx.
//
// The SDK always requires ctx for setting data, but globalScope
// is the fallback to be used when no scope was found on the passed ctx.
var globalScope = newScope()

type scopeContextKey struct{}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func contextWithScope(ctx context.Context, scope scope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

func scopeFromContext(ctx context.Context) scope {
	ctx = normalizeContext(ctx)
	if ctx == nil {
		return newScopeWithClient(globalScope.client)
	}
	if scope, ok := ctx.Value(scopeContextKey{}).(scope); ok {
		return scope.clone()
	}
	if hub := GetHubFromContext(ctx); hub != nil {
		return newScopeWithClient(hub.Client())
	}
	return newScopeWithClient(globalScope.client)
}

func (scope scope) captureMessage(message string) *EventID {
	if scope.client == nil {
		return nil
	}
	return scope.client.CaptureMessage(message, nil, scopeEventModifier{scope: scope})
}

func (scope scope) captureException(exception error) *EventID {
	if scope.client == nil {
		return nil
	}
	return scope.client.CaptureException(exception, &EventHint{OriginalException: exception}, scopeEventModifier{scope: scope})
}

func (scope scope) captureEvent(event *Event) *EventID {
	if scope.client == nil {
		return nil
	}
	return scope.client.CaptureEvent(event, nil, scopeEventModifier{scope: scope})
}

func (scope scope) captureCheckIn(checkIn *CheckIn, monitorConfig *MonitorConfig) *EventID {
	if scope.client == nil {
		return nil
	}
	return scope.client.CaptureCheckIn(checkIn, monitorConfig, scopeEventModifier{scope: scope})
}

func (scope scope) recover(ctx context.Context) *EventID {
	err := recover()
	if err == nil || scope.client == nil {
		return nil
	}
	return scope.client.RecoverWithContext(ctx, err, &EventHint{RecoveredException: err}, scopeEventModifier{scope: scope})
}

func newScope() scope {
	return newScopeWithClient(nil)
}

func newScopeWithClient(client *Client) scope {
	return scope{
		attachments:     make([]*Attachment, 0),
		attributes:      make(map[string]attribute.Value),
		breadcrumbs:     make([]*Breadcrumb, 0),
		client:          client,
		contexts:        make(map[string]Context),
		eventProcessors: make([]EventProcessor, 0),
		fingerprint:     make([]string, 0),
		tags:            make(map[string]string),
	}
}

// scope is the information carrier.
type scope struct {
	attachments     []*Attachment
	attributes      map[string]attribute.Value
	breadcrumbs     []*Breadcrumb
	client          *Client
	contexts        map[string]Context
	eventProcessors []EventProcessor
	fingerprint     []string
	level           Level
	request         *http.Request
	// requestBody holds a reference to the original request.Body.
	requestBody interface {
		// Bytes returns bytes from the original body, lazily buffered as the
		// original body is read.
		Bytes() []byte
		// Overflow returns true if the body is larger than the maximum buffer
		// size.
		Overflow() bool
	}
	tags map[string]string
	user User
}

func (scope scope) clone() scope {
	clone := newScope()
	clone.attachments = cloneAttachments(scope.attachments)
	clone.attributes = maps.Clone(scope.attributes)
	clone.breadcrumbs = cloneBreadcrumbs(scope.breadcrumbs)
	clone.client = scope.client
	clone.contexts = cloneContexts(scope.contexts)
	clone.eventProcessors = append(clone.eventProcessors[:0], scope.eventProcessors...)
	clone.fingerprint = append(clone.fingerprint[:0], scope.fingerprint...)
	clone.level = scope.level
	clone.request = scope.request
	clone.requestBody = scope.requestBody
	clone.tags = maps.Clone(scope.tags)
	clone.user = cloneUser(scope.user)
	return clone
}

func (scope *scope) setUser(user User) {
	scope.user = cloneUser(user)
}

func (scope *scope) setRequest(r *http.Request) {
	scope.request = r
	scope.requestBody = nil

	if r == nil {
		return
	}
	if r.ContentLength > maxRequestBodyBytes {
		return
	}
	if r.Body == nil || r.Body == http.NoBody {
		return
	}

	buf := &limitedBuffer{Capacity: maxRequestBodyBytes}
	r.Body = readCloser{
		Reader: io.TeeReader(r.Body, buf),
		Closer: r.Body,
	}
	scope.requestBody = buf
}

func (scope *scope) setRequestBody(b []byte) {
	capacity := maxRequestBodyBytes
	overflow := false
	if len(b) > capacity {
		overflow = true
		b = b[:capacity]
	}
	scope.requestBody = &limitedBuffer{
		Capacity: capacity,
		Buffer:   *bytes.NewBuffer(bytes.Clone(b)),
		overflow: overflow,
	}
}

func (scope *scope) setAttributes(attrs ...attribute.Builder) {
	for _, a := range attrs {
		if a.Value.Type() == attribute.INVALID {
			debuglog.Printf("invalid attribute: %v", a)
			continue
		}
		scope.attributes[a.Key] = a.Value
	}
}

func (scope *scope) removeAttribute(key string) {
	delete(scope.attributes, key)
}

func (scope *scope) setTag(key, value string) {
	scope.tags[key] = value
}

func (scope *scope) setTags(tags map[string]string) {
	maps.Copy(scope.tags, tags)
}

func (scope *scope) setContext(key string, value Context) {
	scope.contexts[key] = cloneContext(value)
}

func (scope *scope) setContexts(contexts map[string]Context) {
	for key, value := range contexts {
		scope.contexts[key] = cloneContext(value)
	}
}

func (scope *scope) setFingerprint(fingerprint []string) {
	scope.fingerprint = append(scope.fingerprint[:0], fingerprint...)
}

func (scope *scope) setLevel(level Level) {
	scope.level = level
}

func (scope *scope) setBreadcrumb(breadcrumb *Breadcrumb) {
	if breadcrumb == nil {
		return
	}

	limit := defaultMaxBreadcrumbs
	if scope.client != nil {
		clientLimit := scope.client.options.MaxBreadcrumbs
		switch {
		case clientLimit < 0:
			return
		case clientLimit > 0:
			limit = clientLimit
		}

		if scope.client.options.BeforeBreadcrumb != nil {
			breadcrumb = cloneBreadcrumb(breadcrumb)
			breadcrumb = scope.client.options.BeforeBreadcrumb(breadcrumb, &BreadcrumbHint{})
			if breadcrumb == nil {
				return
			}
		}
	}

	breadcrumb = cloneBreadcrumb(breadcrumb)
	if breadcrumb.Timestamp.IsZero() {
		breadcrumb.Timestamp = time.Now()
	}

	scope.breadcrumbs = append(scope.breadcrumbs, breadcrumb)
	if len(scope.breadcrumbs) > limit {
		scope.breadcrumbs = scope.breadcrumbs[1 : limit+1]
	}
}

func (scope *scope) addEventProcessor(processor EventProcessor) {
	scope.eventProcessors = append(scope.eventProcessors, processor)
}

// scopeEventModifier is a compatibility layer to convert the new scope to *Scope.
// It is currently used to not change the event pipeline for now.
type scopeEventModifier struct {
	scope scope
}

func (m scopeEventModifier) ApplyToEvent(event *Event, hint *EventHint, client *Client) *Event {
	// apply global scope first. This is quite suboptimal but it's a temporary step, so fine for now.
	event = getLegacyScope(globalScope).ApplyToEvent(event, hint, client)
	return getLegacyScope(m.scope).ApplyToEvent(event, hint, client)
}

func getLegacyScope(s scope) *Scope {
	legacy := NewScope()
	legacy.attachments = cloneAttachments(s.attachments)
	legacy.attributes = s.attributes
	legacy.breadcrumbs = cloneBreadcrumbs(s.breadcrumbs)
	legacy.contexts = cloneContexts(s.contexts)
	legacy.eventProcessors = append([]EventProcessor(nil), s.eventProcessors...)
	legacy.fingerprint = append([]string(nil), s.fingerprint...)
	legacy.level = s.level
	legacy.request = s.request
	legacy.requestBody = s.requestBody
	legacy.tags = maps.Clone(s.tags)
	legacy.user = cloneUser(s.user)
	return legacy
}
