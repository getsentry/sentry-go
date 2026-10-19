package sentry

import (
	"context"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
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
	requestBody requestBody

	propagationContext PropagationContext
	// span is the root last attached to this scope, read by the Hub API.
	span *Span
}

type requestBody interface {
	// Bytes returns bytes from the original body, lazily buffered as the
	// original body is read.
	Bytes() []byte
	// Overflow returns true if the body is larger than the maximum buffer
	// size.
	Overflow() bool
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

	if !frozenDSCMatchesTrace(propagationContext.DynamicSamplingContext, propagationContext.TraceID) {
		propagationContext.DynamicSamplingContext = DynamicSamplingContext{Frozen: true}
	}
	scope.propagationContext = propagationContext.clone()
}

func (scope *Scope) propagationContextSnapshot() PropagationContext {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	return scope.propagationContext.clone()
}

// keepSamplingDecision records the sampling decision and local sample rate of a
// root that continues the scope's trace, so spanless telemetry agrees with it.
func (scope *Scope) keepSamplingDecision(traceID TraceID, sampled Sampled, dsc DynamicSamplingContext) {
	if scope == GlobalScope() {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()
	propagation := &scope.propagationContext
	if propagation.TraceID != traceID {
		return
	}
	if propagation.Sampled == SampledUndefined {
		propagation.Sampled = sampled
	}
	rate, ok := dsc.Entries["sample_rate"]
	scopeDSC := propagation.DynamicSamplingContext
	if !ok || !scopeDSC.IsFrozen() || !scopeDSC.HasEntries() {
		return
	}
	if _, set := scopeDSC.Entries["sample_rate"]; !set {
		entries := maps.Clone(scopeDSC.Entries)
		entries["sample_rate"] = rate
		propagation.DynamicSamplingContext.Entries = entries
	}
}

func (scope *Scope) propagationContextForPropagation(client *Client) PropagationContext {
	client = normalizeClient(client)
	scope.mu.Lock()
	defer scope.mu.Unlock()

	freezeDynamicSamplingContext(&scope.propagationContext, client)
	return scope.propagationContext.clone()
}

// freezeDynamicSamplingContext initializes the sampling metadata of a trace
// that has not been propagated yet.
func freezeDynamicSamplingContext(propagationContext *PropagationContext, client *Client) {
	if !client.IsEnabled() || propagationContext.TraceID == zeroTraceID || propagationContext.DynamicSamplingContext.IsFrozen() {
		return
	}
	applyStaticSamplingDecision(propagationContext, client)
	dsc := dynamicSamplingContextFromPropagationContext(*propagationContext, client)
	if client.options.EnableTracing && propagationContext.Sampled == SampledFalse {
		dsc.Entries["sampled"] = "false"
	}
	propagationContext.DynamicSamplingContext = dsc
}

func applyStaticSamplingDecision(propagationContext *PropagationContext, client *Client) {
	if client.options.EnableTracing && client.options.TracesSampler == nil && client.options.TracesSampleRate == 0 {
		if propagationContext.Sampled == SampledUndefined {
			propagationContext.Sampled = SampledFalse
		}
	}
}

// GetSpan returns the span attached to the current scope. Use [SpanFromContext]
// for the active span.
func (scope *Scope) GetSpan() *Span {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
	return scope.span
}

// SetSpan attaches span to the current scope.
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
	snapshot, processors := scope.copyToEvent(event, maxBreadcrumbs)
	applyRequestToEvent(event, client, snapshot.request, snapshot.requestBody)
	_, explicitTrace := event.Contexts[traceContextKey]
	applyTraceToEvent(ctx, event, client, scope, snapshot.propagationContext, explicitTrace)
	return processors
}

// applyScopesToEvent merges the global scope and the context scope into event. Data applies
// from more specific to less specific. A nil scope only applies data from the global one.
func applyScopesToEvent(
	ctx context.Context,
	event *Event,
	client *Client,
	scope *Scope,
	maxBreadcrumbs int,
) (globalProcessors, scopeProcessors []EventProcessor) {
	global := GlobalScope()
	if scope == global {
		scope = nil
	}

	// The more specific scope goes first: every field only fills what is
	// still missing, and slices are prepended, so no intermediate scope is built.
	var snapshot scopeData
	if scope != nil {
		snapshot, scopeProcessors = scope.copyToEvent(event, maxBreadcrumbs)
	}
	globalSnapshot, globalProcessors := global.copyToEvent(event, maxBreadcrumbs)
	if scope == nil {
		// The global scope's trace is never used.
		snapshot = globalSnapshot
		snapshot.propagationContext = PropagationContext{}
	} else if snapshot.request == nil {
		snapshot.request, snapshot.requestBody = globalSnapshot.request, globalSnapshot.requestBody
	}
	applyRequestToEvent(event, client, snapshot.request, snapshot.requestBody)

	_, explicitTrace := event.Contexts[traceContextKey]
	applyTraceToEvent(ctx, event, client, scope, snapshot.propagationContext, explicitTrace)
	return globalProcessors, scopeProcessors
}

func applyRequestToEvent(event *Event, client *Client, request *http.Request, body requestBody) {
	if event.Request != nil || request == nil {
		return
	}
	event.Request = newRequest(request, client)
	dc := client.GetDataCollection()
	if body != nil && !body.Overflow() && dc.CollectHTTPBody(BodyIncomingRequest) {
		event.Request.Data = dc.FilterHTTPBody(body.Bytes(), request.Header.Get("Content-Type"))
	}
}

func (scope *Scope) copyToEvent(event *Event, maxBreadcrumbs int) (scopeData, []EventProcessor) {
	scope.mu.RLock()
	defer scope.mu.RUnlock()
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

	return scope.scopeData, scope.eventProcessors[:len(scope.eventProcessors):len(scope.eventProcessors)]
}

func applyTraceToEvent(ctx context.Context, event *Event, client *Client, scope *Scope, propagation PropagationContext, explicit bool) {
	if event.Type == transactionType {
		return
	}
	// forPropagation initializes the trace's sampling metadata, on scope when
	// there is one so that later captures and outgoing requests reuse it.
	forPropagation := func() PropagationContext {
		if scope != nil {
			return scope.propagationContextForPropagation(client)
		}
		freezeDynamicSamplingContext(&propagation, normalizeClient(client))
		return propagation
	}
	if !explicit {
		trace := activeTraceFromContexts(client, ctx)
		if scope == nil && trace.traceID == zeroTraceID && trace.span == nil {
			// Nothing carries a trace, so the event starts its own.
			propagation = NewPropagationContext()
		}
		trace = trace.withScope(propagation)
		switch {
		case trace.span != nil:
			setEventTrace(event, trace.span.traceContext().Map())
		case trace.traceID == zeroTraceID:
			return
		case trace.propagation:
			propagation = forPropagation()
			if propagation.TraceID == zeroTraceID {
				return
			}
			setEventTrace(event, propagation.Map())
			if !event.sdkMetaData.dsc.HasEntries() && !event.sdkMetaData.dsc.IsFrozen() {
				event.sdkMetaData.dsc = propagation.DynamicSamplingContext
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
	event.sdkMetaData.dsc = matchingDSC(traceID, propagation, SpanFromContext(ctx))
	if !event.sdkMetaData.dsc.HasEntries() && !event.sdkMetaData.dsc.IsFrozen() && propagation.TraceID != zeroTraceID && strings.EqualFold(traceID, propagation.TraceID.String()) {
		// Initialize the selected scope trace's DSC just as for propagation.
		// Recheck the trace in case the scope changed since the snapshot.
		propagation = forPropagation()
		event.sdkMetaData.dsc = matchingDSC(traceID, propagation)
	}
}

// matchingDSC keeps sampling metadata from the selected trace only.
func matchingDSC(traceID string, propagation PropagationContext, roots ...*Span) DynamicSamplingContext {
	for _, root := range roots {
		if root != nil && strings.EqualFold(traceID, root.TraceID.String()) {
			dsc := root.dynamicSamplingContextForPropagation()
			if dsc.HasEntries() || dsc.IsFrozen() {
				return dsc
			}
		}
	}
	dsc := propagation.DynamicSamplingContext
	if traceID != "" && strings.EqualFold(traceID, dsc.Entries[traceIDContextKey]) {
		return dsc
	}
	if traceID != "" && dsc.IsFrozen() && dsc.Entries[traceIDContextKey] == "" && strings.EqualFold(traceID, propagation.TraceID.String()) {
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
		return event
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

func mergeScopeAttributes(
	client *Client,
	scope *Scope,
	extraCapacity int,
) map[string]attribute.Value {
	client = normalizeClient(client)
	serverAddress := client.options.ServerName
	if serverAddress == "" {
		serverAddress = hostname
	}
	sdkName := client.GetSDKIdentifier()
	global := GlobalScope()
	if scope == global {
		scope = nil
	}

	scopeAttributes := 0
	if scope != nil {
		scope.mu.RLock()
		scopeAttributes = len(scope.attributes)
		scope.mu.RUnlock()
	}
	global.mu.RLock()
	attrs := make(map[string]attribute.Value, len(global.attributes)+scopeAttributes+extraCapacity+8)
	setString := func(key, value string) {
		if value != "" {
			attrs[key] = attribute.StringValue(value)
		}
	}
	setString("sentry.release", client.options.Release)
	setString("sentry.environment", client.options.Environment)
	setString("sentry.server.address", serverAddress)
	setString("sentry.sdk.name", sdkName)
	setString("sentry.sdk.version", client.sdkVersion)
	user := global.user
	maps.Copy(attrs, global.attributes)
	global.mu.RUnlock()

	if scope != nil {
		scope.mu.RLock()
		if !scope.user.IsEmpty() {
			user = scope.user
		}
		maps.Copy(attrs, scope.attributes)
		scope.mu.RUnlock()
	}
	for key, value := range map[string]string{"user.id": user.ID, "user.name": user.Name, "user.email": user.Email} {
		if _, ok := attrs[key]; !ok {
			setString(key, value)
		}
	}
	return attrs
}

type activeTrace struct {
	traceID     TraceID
	spanID      SpanID
	span        *Span
	propagation bool
	sampled     Sampled
}

func activeTraceFromContexts(client *Client, ctxs ...context.Context) activeTrace {
	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
		if traceID, spanID, sampled, ok := client.externalTraceContextFromContext(ctx); ok && traceID != zeroTraceID && spanID != zeroSpanID {
			trace := activeTrace{traceID: traceID, spanID: spanID, sampled: sampled}
			if span := SpanFromContext(ctx); span != nil && span.TraceID == traceID && span.SpanID == spanID {
				trace.span = span
			}
			return trace
		}
		if span := SpanFromContext(ctx); span != nil {
			return activeTraceFromSpan(span)
		}
	}
	return activeTrace{}
}

func activeTraceFromSpan(span *Span) activeTrace {
	span.mu.RLock()
	defer span.mu.RUnlock()
	return activeTrace{traceID: span.TraceID, spanID: span.SpanID, span: span, sampled: span.Sampled}
}

// withScope supplies the scope fallback without changing an active trace selection.
func (trace activeTrace) withScope(propagation PropagationContext) activeTrace {
	if trace.span == nil && trace.traceID == zeroTraceID {
		trace = activeTrace{traceID: propagation.TraceID, spanID: propagation.SpanID, sampled: propagation.Sampled, propagation: true}
	}
	return trace
}

func resolveTrace(scope *Scope, client *Client, ctxs ...context.Context) (TraceID, SpanID) {
	trace := activeTraceFromContexts(client, ctxs...)
	if trace.traceID == zeroTraceID && trace.span == nil {
		if scope == nil {
			// Neither a span nor a scope carries a trace, so start one.
			return NewPropagationContext().TraceID, zeroSpanID
		}
		scope.mu.RLock()
		trace = trace.withScope(scope.propagationContext)
		scope.mu.RUnlock()
	}
	if trace.propagation {
		return trace.traceID, zeroSpanID
	}
	return trace.traceID, trace.spanID
}
