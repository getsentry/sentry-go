package sentry

import (
	"context"
	"io"
	"maps"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/httputils"
)

// Scope holds contextual data for the current scope.
//
// The scope is an object that can cloned efficiently and stores data that is
// locally relevant to an event. For instance the scope will hold recorded
// breadcrumbs and similar information.
//
// The scope can be interacted with in two ways. First, the scope is routinely
// updated with information by functions such as AddBreadcrumb which will modify
// the current scope. Second, the current scope can be configured through the
// ConfigureScope function or Hub method of the same name.
//
// The scope is meant to be modified but not inspected directly. When preparing
// an event for reporting, the current client adds information from the current
// scope into the event.
type Scope struct {
	mu sync.RWMutex
	// eventProcessors are retained by Clear and inherited by Clone.
	eventProcessors []EventProcessor

	lastEventMu sync.Mutex
	lastEventID EventID

	// scopeData keeps track of all scope specific data
	scopeData
}

type scopeData struct {
	attributes  map[string]attribute.Value
	breadcrumbs []*Breadcrumb
	attachments []*Attachment
	user        User
	tags        map[string]string
	contexts    map[string]Context
	fingerprint []string
	level       Level
	request     *http.Request
	// requestBody holds a reference to the original request.Body.
	requestBody interface {
		// Bytes returns bytes from the original body, lazily buffered as the
		// original body is read.
		Bytes() []byte
		// Overflow returns true if the body is larger than the maximum buffer
		// size.
		Overflow() bool
	}

	propagationContext PropagationContext
	// span is the root transaction most recently attached to this scope. It is
	// a propagation fallback only: parent selection is always context-based.
	span *Span
}

// NewScope creates a new Scope.
func NewScope() *Scope {
	return &Scope{scopeData: newScopeData(NewPropagationContext())}
}

func newScopeData(propagationContext PropagationContext) scopeData {
	return scopeData{
		attributes:         make(map[string]attribute.Value),
		breadcrumbs:        make([]*Breadcrumb, 0),
		attachments:        make([]*Attachment, 0),
		tags:               make(map[string]string),
		contexts:           make(map[string]Context),
		fingerprint:        make([]string, 0),
		propagationContext: propagationContext,
	}
}

// AddBreadcrumb adds new breadcrumb to the current scope
// and optionally throws the old one if limit is reached.
func (scope *Scope) AddBreadcrumb(breadcrumb *Breadcrumb, limit int) {
	if breadcrumb.Timestamp.IsZero() {
		breadcrumb.Timestamp = time.Now()
	}

	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.breadcrumbs = append(scope.breadcrumbs, breadcrumb)
	if len(scope.breadcrumbs) > limit {
		scope.breadcrumbs = scope.breadcrumbs[1 : limit+1]
	}
}

func (scope *Scope) setLastEventID(id EventID) {
	scope.lastEventMu.Lock()
	defer scope.lastEventMu.Unlock()

	scope.lastEventID = id
}

func (scope *Scope) lastEventIDSnapshot() EventID {
	scope.lastEventMu.Lock()
	defer scope.lastEventMu.Unlock()

	return scope.lastEventID
}

// ClearBreadcrumbs clears all breadcrumbs from the current scope.
func (scope *Scope) ClearBreadcrumbs() {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.breadcrumbs = []*Breadcrumb{}
}

// AddAttachment adds new attachment to the current scope.
func (scope *Scope) AddAttachment(attachment *Attachment) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.attachments = append(scope.attachments, attachment)
}

// ClearAttachments clears all attachments from the current scope.
func (scope *Scope) ClearAttachments() {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.attachments = []*Attachment{}
}

// SetUser sets the user for the current scope.
func (scope *Scope) SetUser(user User) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.user = user
}

// SetRequest sets the request for the current scope.
func (scope *Scope) SetRequest(r *http.Request) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.request = r

	if r == nil {
		return
	}

	// Don't buffer request body if we know it is oversized.
	if r.ContentLength > httputils.MaxBodyBytes {
		return
	}
	// Don't buffer if there is no body.
	if r.Body == nil || r.Body == http.NoBody {
		return
	}
	buf := httputils.NewLimitedBuffer(httputils.MaxBodyBytes)
	r.Body = httputils.ReadCloser{
		Reader: io.TeeReader(r.Body, buf),
		Closer: r.Body,
	}
	scope.requestBody = buf
}

// SetRequestBody sets the request body for the current scope.
//
// This method should only be called when the body bytes are already available
// in memory. Typically, the request body is buffered lazily from the
// Request.Body from SetRequest.
func (scope *Scope) SetRequestBody(b []byte) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.requestBody = httputils.NewLimitedBufferFromBytes(httputils.MaxBodyBytes, b)
}

// SetAttributes adds attributes to the current scope.
func (scope *Scope) SetAttributes(attrs ...attribute.Builder) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	for _, a := range attrs {
		if a.Value.Type() == attribute.INVALID {
			debuglog.Printf("invalid attribute: %v", a)
			continue
		}
		scope.attributes[a.Key] = a.Value
	}
}

// RemoveAttribute removes an attribute from the current scope.
func (scope *Scope) RemoveAttribute(key string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	delete(scope.attributes, key)
}

// SetTag adds a tag to the current scope.
func (scope *Scope) SetTag(key, value string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.tags[key] = value
}

// SetTags assigns multiple tags to the current scope.
func (scope *Scope) SetTags(tags map[string]string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	for k, v := range tags {
		scope.tags[k] = v
	}
}

// RemoveTag removes a tag from the current scope.
func (scope *Scope) RemoveTag(key string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	delete(scope.tags, key)
}

// SetContext adds a context to the current scope.
func (scope *Scope) SetContext(key string, value Context) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.contexts[key] = value
}

// SetContexts assigns multiple contexts to the current scope.
func (scope *Scope) SetContexts(contexts map[string]Context) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	for k, v := range contexts {
		scope.contexts[k] = v
	}
}

// RemoveContext removes a context from the current scope.
func (scope *Scope) RemoveContext(key string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	delete(scope.contexts, key)
}

// SetFingerprint sets new fingerprint for the current scope.
func (scope *Scope) SetFingerprint(fingerprint []string) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.fingerprint = fingerprint
}

// SetLevel sets new level for the current scope.
func (scope *Scope) SetLevel(level Level) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.level = level
}

// SetPropagationContext sets the propagation context for the current scope.
func (scope *Scope) SetPropagationContext(propagationContext PropagationContext) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.propagationContext = propagationContext.clone()
}

func (scope *Scope) propagationContextSnapshot() PropagationContext {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	return scope.propagationContext.clone()
}

// GetSpan returns the span attached to the current scope. The SDK attaches a
// request root for propagation and capture fallback; use [SpanFromContext] to
// retrieve the active child span.
func (scope *Scope) GetSpan() *Span {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	return scope.span
}

// SetSpan attaches span to the current scope. SDK-created roots use this as a
// propagation fallback; callers can attach any span.
func (scope *Scope) SetSpan(span *Span) {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	scope.span = span
}

// Clone returns a copy of the current scope without its last event ID.
func (scope *Scope) Clone() *Scope {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	clone := NewScope()
	clone.user = scope.user
	clone.user.Data = maps.Clone(scope.user.Data)
	clone.breadcrumbs = make([]*Breadcrumb, len(scope.breadcrumbs))
	copy(clone.breadcrumbs, scope.breadcrumbs)
	clone.attachments = make([]*Attachment, len(scope.attachments))
	copy(clone.attachments, scope.attachments)
	clone.attributes = maps.Clone(scope.attributes)
	clone.contexts = maps.Clone(scope.contexts)
	clone.tags = maps.Clone(scope.tags)
	clone.fingerprint = make([]string, len(scope.fingerprint))
	copy(clone.fingerprint, scope.fingerprint)
	clone.level = scope.level
	clone.request = scope.request
	clone.requestBody = scope.requestBody
	clone.eventProcessors = scope.eventProcessors[:len(scope.eventProcessors):len(scope.eventProcessors)]
	clone.propagationContext = scope.propagationContext.clone()
	clone.span = scope.span
	return clone
}

// Clear removes enrichment data while preserving event processors, trace
// correlation, and the last event ID. It is safe for concurrent use.
func (scope *Scope) Clear() {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	propagationContext := scope.propagationContext
	span := scope.span
	scope.scopeData = newScopeData(propagationContext)
	scope.span = span
}

// AddEventProcessor adds an event processor to the current scope.
func (scope *Scope) AddEventProcessor(processor EventProcessor) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.eventProcessors = append(scope.eventProcessors, processor)
}

// ApplyToEvent takes the data from the current scope and attaches it to the event.
func (scope *Scope) ApplyToEvent(event *Event, hint *EventHint, client *Client) *Event {
	client = normalizeClient(client)
	var ctx context.Context
	if hint != nil {
		ctx = hint.Context
	}
	processors := scope.applyToEvent(ctx, event, client, client.options.MaxBreadcrumbs)
	return client.runEventProcessors(event, hint, processors)
}

// applyToEvent applies the effective scope to event and returns the scope
// processors so they can run after the scope lock is released.
func (scope *Scope) applyToEvent(
	ctx context.Context,
	event *Event,
	client *Client,
	maxBreadcrumbs int,
) []EventProcessor {
	scope.mu.RLock()
	if len(scope.tags) > 0 {
		if event.Tags == nil {
			event.Tags = make(map[string]string, len(scope.tags))
		}
		for key, value := range scope.tags {
			if _, exists := event.Tags[key]; !exists {
				event.Tags[key] = value
			}
		}
	}
	if len(scope.contexts) > 0 {
		if event.Contexts == nil {
			event.Contexts = make(map[string]Context, len(scope.contexts))
		}
		for key, value := range scope.contexts {
			if key == traceContextKey && event.Type == transactionType {
				continue
			}
			if _, exists := event.Contexts[key]; !exists {
				event.Contexts[key] = cloneContext(value)
			}
		}
	}
	_, explicitTrace := event.Contexts[traceContextKey]

	// Copy slice-backed scope data while holding the read lock. Snapshotting
	// only the slice headers would race with concurrent append operations.
	event.Breadcrumbs = mergeBreadcrumbs(scope.breadcrumbs, event.Breadcrumbs, maxBreadcrumbs)
	event.Attachments = prependSlice(scope.attachments, event.Attachments)
	if event.User.IsEmpty() && !scope.user.IsEmpty() {
		event.User = scope.user
		event.User.Data = maps.Clone(scope.user.Data)
	}
	if len(event.Fingerprint) == 0 && len(scope.fingerprint) > 0 {
		event.Fingerprint = slices.Clone(scope.fingerprint)
	}
	if event.Level == "" {
		event.Level = scope.level
	}

	request := scope.request
	requestBody := scope.requestBody
	propagationContext := scope.propagationContext
	root := scope.span
	processors := scope.eventProcessors[:len(scope.eventProcessors):len(scope.eventProcessors)]
	scope.mu.RUnlock()

	if event.Request == nil && request != nil {
		event.Request = newRequest(request, client)
		dc := client.GetDataCollection()
		if requestBody != nil && !requestBody.Overflow() && dc.CollectHTTPBody(BodyIncomingRequest) {
			event.Request.Data = dc.FilterHTTPBody(
				requestBody.Bytes(),
				request.Header.Get("Content-Type"),
			)
		}
	}

	applyTraceToEvent(ctx, event, client, propagationContext, root, explicitTrace)
	return processors
}

func applyTraceToEvent(ctx context.Context, event *Event, client *Client, propagation PropagationContext, root *Span, explicit bool) {
	if event.Type == transactionType {
		return
	}
	if !explicit {
		trace := activeTraceFromContexts(client, ctx).withScope(root, propagation)
		switch {
		case trace.span != nil:
			setEventTrace(event, trace.span.traceContext().Map())
		case trace.traceID == zeroTraceID:
			return
		case trace.propagation:
			setEventTrace(event, propagation.Map())
			if !event.sdkMetaData.dsc.HasEntries() && !event.sdkMetaData.dsc.IsFrozen() {
				dsc := propagation.DynamicSamplingContext
				if !dsc.HasEntries() && !dsc.IsFrozen() {
					dsc = dynamicSamplingContextFromPropagationContext(propagation, client)
				}
				event.sdkMetaData.dsc = dsc
			}
			return
		default:
			setEventTrace(event, Context{traceIDContextKey: trace.traceID.String(), spanIDContextKey: trace.spanID.String()})
		}
	}
	if event.sdkMetaData.dsc.HasEntries() || event.sdkMetaData.dsc.IsFrozen() {
		return
	}
	var traceID string
	switch id := event.Contexts[traceContextKey][traceIDContextKey].(type) {
	case TraceID:
		traceID = id.String()
	case string:
		traceID = id
	}
	event.sdkMetaData.dsc = matchingDSC(traceID, propagation, SpanFromContext(ctx), root)
}

// matchingDSC keeps sampling metadata from the selected trace only.
func matchingDSC(traceID string, propagation PropagationContext, roots ...*Span) DynamicSamplingContext {
	for _, root := range roots {
		if root != nil && traceID == root.TraceID.String() {
			dsc := root.dynamicSamplingContextForPropagation()
			if dsc.HasEntries() || dsc.IsFrozen() {
				return dsc
			}
		}
	}
	dsc := propagation.DynamicSamplingContext
	if traceID != "" && traceID == dsc.Entries[traceIDContextKey] {
		return dsc
	}
	return DynamicSamplingContext{}
}

func setEventTrace(event *Event, trace Context) {
	if event.Contexts == nil {
		event.Contexts = make(map[string]Context)
	}
	event.Contexts[traceContextKey] = trace
}

func mergeBreadcrumbs(scope, event []*Breadcrumb, limit int) []*Breadcrumb {
	switch {
	case limit < 0:
		return nil
	case limit == 0:
		limit = defaultMaxBreadcrumbs
	}

	event = event[max(0, len(event)-limit):]
	scopeLimit := limit - len(event)
	scope = scope[max(0, len(scope)-scopeLimit):]
	return prependSlice(scope, event)
}

func prependSlice[T any](prefix, suffix []T) []T {
	if len(prefix) == 0 {
		return suffix
	}
	merged := make([]T, len(prefix)+len(suffix))
	copy(merged, prefix)
	copy(merged[len(prefix):], suffix)
	return merged
}

// cloneContext returns a new context with keys and values copied from the passed one.
//
// Note: a new Context (map) is returned, but the function does NOT do
// a proper deep copy: if some context values are pointer types (e.g. maps),
// they won't be properly copied.
func cloneContext(c Context) Context {
	if c == nil {
		return Context{}
	}
	return maps.Clone(c)
}

func (scope *Scope) populateAttrs(attrs map[string]attribute.Value) {
	if scope == nil {
		return
	}

	scope.mu.RLock()
	defer scope.mu.RUnlock()

	// Add user-related attributes
	if !scope.user.IsEmpty() {
		if scope.user.ID != "" {
			attrs["user.id"] = attribute.StringValue(scope.user.ID)
		}
		if scope.user.Name != "" {
			attrs["user.name"] = attribute.StringValue(scope.user.Name)
		}
		if scope.user.Email != "" {
			attrs["user.email"] = attribute.StringValue(scope.user.Email)
		}
	}

	for k, v := range scope.attributes {
		attrs[k] = v
	}
}

// hubFromContexts is a helper to return the first hub found in the given contexts.
func hubFromContexts(ctxs ...context.Context) *Hub {
	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
		if hub := GetHubFromContext(ctx); hub != nil {
			return hub
		}
	}
	return nil
}

type activeTrace struct {
	traceID     TraceID
	spanID      SpanID
	span        *Span
	propagation bool
}

func activeTraceFromContexts(client *Client, ctxs ...context.Context) activeTrace {
	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
		if traceID, spanID, ok := client.externalTraceContextFromContext(ctx); ok {
			trace := activeTrace{traceID: traceID, spanID: spanID}
			if span := SpanFromContext(ctx); span != nil && span.TraceID == traceID && span.SpanID == spanID {
				trace.span = span
			}
			return trace
		}
		if span := SpanFromContext(ctx); span != nil {
			return activeTrace{traceID: span.TraceID, spanID: span.SpanID, span: span}
		}
	}
	return activeTrace{}
}

// withScope supplies the scope fallback without changing an active trace selection.
func (trace activeTrace) withScope(root *Span, propagation PropagationContext) activeTrace {
	if trace.span != nil {
		return trace
	}
	if root != nil {
		if trace.traceID == zeroTraceID {
			return activeTrace{traceID: root.TraceID, spanID: root.SpanID, span: root}
		}
		if trace.traceID == root.TraceID && trace.spanID == root.SpanID {
			trace.span = root
		}
	} else if trace.traceID == zeroTraceID {
		trace = activeTrace{traceID: propagation.TraceID, spanID: propagation.SpanID, propagation: true}
	}
	return trace
}

func resolveTrace(scope *Scope, client *Client, ctxs ...context.Context) (TraceID, SpanID) {
	trace := activeTraceFromContexts(client, ctxs...)
	if trace.traceID == zeroTraceID && trace.span == nil {
		if scope == nil {
			scope = GlobalScope()
		}
		scope.mu.RLock()
		trace = trace.withScope(scope.span, scope.propagationContext)
		scope.mu.RUnlock()
	}
	return trace.traceID, trace.spanID
}
