package sentry

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go/attribute"
)

// The version of the SDK.
const SDKVersion = "0.46.2"

// apiVersion is the minimum version of the Sentry API compatible with the
// sentry-go SDK.
const apiVersion = "7"

// DefaultFlushTimeout is the default timeout used for flushing events.
const DefaultFlushTimeout = 2 * time.Second

// Init initializes the SDK with options. The returned error is non-nil if
// options is invalid, for instance if a malformed DSN is provided.
func Init(options ClientOptions) error {
	hub := CurrentHub()
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	hub.BindClient(client)
	globalScope.client = client
	return nil
}

// AddBreadcrumb records a new breadcrumb on the derived context scope.
func AddBreadcrumb(ctx context.Context, breadcrumb *Breadcrumb) context.Context {
	scope := scopeFromContext(ctx)
	scope.setBreadcrumb(breadcrumb)
	return contextWithScope(ctx, scope)
}

// AddEventProcessor returns a derived context with event processor stored on the current Sentry scope.
func AddEventProcessor(ctx context.Context, processor EventProcessor) context.Context {
	scope := scopeFromContext(ctx)
	scope.addEventProcessor(processor)
	return contextWithScope(ctx, scope)
}

// SetAttributes returns a derived context with attributes stored on the current Sentry scope.
func SetAttributes(ctx context.Context, attrs ...attribute.Builder) context.Context {
	scope := scopeFromContext(ctx)
	scope.setAttributes(attrs...)
	return contextWithScope(ctx, scope)
}

// RemoveAttribute returns a derived context with the attribute removed from the current Sentry scope.
func RemoveAttribute(ctx context.Context, key string) context.Context {
	scope := scopeFromContext(ctx)
	scope.removeAttribute(key)
	return contextWithScope(ctx, scope)
}

// SetContext returns a derived context with the context stored on the current Sentry scope.
func SetContext(ctx context.Context, key string, value Context) context.Context {
	scope := scopeFromContext(ctx)
	scope.setContext(key, value)
	return contextWithScope(ctx, scope)
}

// SetContexts returns a derived context with contexts stored on the current Sentry scope.
func SetContexts(ctx context.Context, contexts map[string]Context) context.Context {
	scope := scopeFromContext(ctx)
	scope.setContexts(contexts)
	return contextWithScope(ctx, scope)
}

// SetFingerprint returns a derived context with fingerprint stored on the current Sentry scope.
func SetFingerprint(ctx context.Context, fingerprint []string) context.Context {
	scope := scopeFromContext(ctx)
	scope.setFingerprint(fingerprint)
	return contextWithScope(ctx, scope)
}

// SetLevel returns a derived context with level stored on the current Sentry scope.
func SetLevel(ctx context.Context, level Level) context.Context {
	scope := scopeFromContext(ctx)
	scope.setLevel(level)
	return contextWithScope(ctx, scope)
}

// SetRequest returns a derived context with request stored on the current Sentry scope.
func SetRequest(ctx context.Context, r *http.Request) context.Context {
	scope := scopeFromContext(ctx)
	scope.setRequest(r)
	return contextWithScope(ctx, scope)
}

// SetRequestBody returns a derived context with request body stored on the current Sentry scope.
func SetRequestBody(ctx context.Context, b []byte) context.Context {
	scope := scopeFromContext(ctx)
	scope.setRequestBody(b)
	return contextWithScope(ctx, scope)
}

// SetTag returns a derived context with the tag stored on the current Sentry scope.
func SetTag(ctx context.Context, key, value string) context.Context {
	scope := scopeFromContext(ctx)
	scope.setTag(key, value)
	return contextWithScope(ctx, scope)
}

// SetTags returns a derived context with tags stored on the current Sentry scope.
func SetTags(ctx context.Context, tags map[string]string) context.Context {
	scope := scopeFromContext(ctx)
	scope.setTags(tags)
	return contextWithScope(ctx, scope)
}

// SetUser returns a derived context with user stored on the current Sentry scope.
func SetUser(ctx context.Context, user User) context.Context {
	scope := scopeFromContext(ctx)
	scope.setUser(user)
	return contextWithScope(ctx, scope)
}

// SetGlobalAttributes sets process-wide default attributes.
func SetGlobalAttributes(attrs ...attribute.Builder) {
	globalScope.setAttributes(attrs...)
}

// SetGlobalContext sets a process-wide default context entry.
func SetGlobalContext(key string, value Context) {
	globalScope.setContext(key, value)
}

// SetGlobalContexts sets process-wide default context entries.
func SetGlobalContexts(contexts map[string]Context) {
	globalScope.setContexts(contexts)
}

// SetGlobalFingerprint sets the process-wide default fingerprint.
func SetGlobalFingerprint(fingerprint []string) {
	globalScope.setFingerprint(fingerprint)
}

// SetGlobalLevel sets the process-wide default level.
func SetGlobalLevel(level Level) {
	globalScope.setLevel(level)
}

// SetGlobalTag sets a process-wide default tag.
func SetGlobalTag(key, value string) {
	globalScope.setTag(key, value)
}

// SetGlobalTags sets process-wide default tags.
func SetGlobalTags(tags map[string]string) {
	globalScope.setTags(tags)
}

// SetGlobalUser sets the process-wide default user.
func SetGlobalUser(user User) {
	globalScope.setUser(user)
}

// CaptureMessage captures an arbitrary message.
func CaptureMessage(ctx context.Context, message string) *EventID {
	return scopeFromContext(ctx).captureMessage(message)
}

// CaptureException captures an error.
func CaptureException(ctx context.Context, exception error) *EventID {
	return scopeFromContext(ctx).captureException(exception)
}

// CaptureCheckIn captures a (cron) monitor check-in.
func CaptureCheckIn(ctx context.Context, checkIn *CheckIn, monitorConfig *MonitorConfig) *EventID {
	return scopeFromContext(ctx).captureCheckIn(checkIn, monitorConfig)
}

// CaptureEvent captures an event on the currently active client if any.
//
// The event must already be assembled. Typically code would instead use
// the utility methods like CaptureException. The return value is the
// event ID. In case Sentry is disabled or event was dropped, the return value will be nil.
func CaptureEvent(ctx context.Context, event *Event) *EventID {
	return scopeFromContext(ctx).captureEvent(event)
}

// Recover captures a panic.
func Recover(ctx context.Context) *EventID {
	return scopeFromContext(ctx).recover(ctx)
}

// WithScope batches multiple scope mutations on a single derived context.
//
// Usage:
//
//	ctx = sentry.WithScope(ctx, func(s *sentry.ContextBuilder) {
//		s.SetTag("key", "value")
//		s.SetUser(sentry.User{ID: "123"})
//	})
//
// TODO: function signature should be f func(*Scope)
// currently naming it ScopeBuilder to avoid breaking the old Scope.
func WithScope(ctx context.Context, f func(*ScopeBuilder)) context.Context {
	b := &ScopeBuilder{ctx: normalizeContext(ctx)}
	f(b)
	return b.ctx
}

// ConfigureScope is a shorthand for CurrentHub().ConfigureScope.
func ConfigureScope(f func(scope *Scope)) {
	hub := CurrentHub()
	hub.ConfigureScope(f)
}

// PushScope is a shorthand for CurrentHub().PushScope.
func PushScope() {
	hub := CurrentHub()
	hub.PushScope()
}

// PopScope is a shorthand for CurrentHub().PopScope.
func PopScope() {
	hub := CurrentHub()
	hub.PopScope()
}

// Flush waits until the underlying Transport sends any buffered events to the
// Sentry server, blocking for at most the given timeout. It returns false if
// the timeout was reached. In that case, some events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Do not call Flush indiscriminately after every call to CaptureEvent,
// CaptureException or CaptureMessage. Instead, to have the SDK send events over
// the network synchronously, configure it to use the HTTPSyncTransport in the
// call to Init.
func Flush(timeout time.Duration) bool {
	hub := CurrentHub()
	return hub.Flush(timeout)
}

// FlushWithContext waits until the underlying Transport sends any buffered events
// to the Sentry server, blocking for at most the duration specified by the context.
// It returns false if the context is canceled before the events are sent. In such a case,
// some events may not be delivered.
//
// FlushWithContext should be called before terminating the program to ensure no
// events are unintentionally dropped.
//
// Avoid calling FlushWithContext indiscriminately after each call to CaptureEvent,
// CaptureException, or CaptureMessage. To send events synchronously over the network,
// configure the SDK to use HTTPSyncTransport during initialization with Init.

func FlushWithContext(ctx context.Context) bool {
	hub := CurrentHub()
	return hub.FlushWithContext(ctx)
}

// LastEventID returns an ID of last captured event.
func LastEventID() EventID {
	hub := CurrentHub()
	return hub.LastEventID()
}
