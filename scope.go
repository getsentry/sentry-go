package sentry

import (
	"context"
	"io"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/httputils"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/util"
	"github.com/getsentry/sentry-go/report"
)

// Scope holds contextual data for an operation.
//
// The scope is an object that can be cloned efficiently and stores data that is
// locally relevant to an event. It also holds the client and event processor in
// which the scope data should be applied to.
//
// Clearing or cloning the scope only affects the underlying data. To set a new
// client or event processor, SetClient or AddEventProcessor should be used.
type Scope struct {
	mu sync.RWMutex
	// clientOverride is an explicit client binding set with SetClient. Having
	// no override defaults to the global scope client.
	clientOverride *Client
	// eventProcessors are retained by Clear and inherited by Clone.
	eventProcessors []EventProcessor
	lastEventID     EventID

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
	span               *Span // TODO: this should be removed when the span API is introduced. Currently kept for compatibility.
}

// NewScope creates a new Scope.
func NewScope() *Scope {
	return &Scope{scopeData: newScopeData()}
}

// newScopeWithClient creates a Scope with an explicit client override.
func newScopeWithClient(client *Client) *Scope {
	scope := NewScope()
	scope.SetClient(client)
	return scope
}

func newScopeData() scopeData {
	return scopeData{
		attributes:         make(map[string]attribute.Value),
		breadcrumbs:        make([]*Breadcrumb, 0),
		attachments:        make([]*Attachment, 0),
		tags:               make(map[string]string),
		contexts:           make(map[string]Context),
		fingerprint:        make([]string, 0),
		propagationContext: NewPropagationContext(),
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

// SetClient sets an explicit client override on the scope. Passing nil clears
// the override so client resolution falls back to GlobalScope.
func (scope *Scope) SetClient(client *Client) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.clientOverride = client
}

func (scope *Scope) clientOverrideSnapshot() *Client {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	return scope.clientOverride
}

// Client returns the first enabled client in the scope chain.
func (scope *Scope) Client() *Client {
	if scope != nil {
		if client := normalizeClient(scope.clientOverrideSnapshot()); client.IsEnabled() {
			return client
		}
	}

	global := GlobalScope()
	if scope != global {
		if client := normalizeClient(global.clientOverrideSnapshot()); client.IsEnabled() {
			return client
		}
	}
	return NewNoopClient()
}

func (scope *Scope) setLastEventID(id EventID) {
	if scope == nil {
		return
	}
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.lastEventID = id
}

// LastEventID returns the last event ID associated with this scope.
func (scope *Scope) LastEventID() EventID {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

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

// GetSpan returns the span from the current scope.
func (scope *Scope) GetSpan() *Span {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	return scope.span
}

// SetSpan sets a span for the current scope.
func (scope *Scope) SetSpan(span *Span) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.span = span
}

// Clone returns a copy of the current scope with all data copied over.
func (scope *Scope) Clone() *Scope {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	data := scope.scopeData
	return &Scope{
		scopeData:       data.clone(),
		eventProcessors: scope.eventProcessors[:len(scope.eventProcessors):len(scope.eventProcessors)],
		clientOverride:  scope.clientOverride,
		lastEventID:     scope.lastEventID,
	}
}

func (data scopeData) clone() scopeData {
	clone := data
	clone.breadcrumbs = make([]*Breadcrumb, len(data.breadcrumbs))
	copy(clone.breadcrumbs, data.breadcrumbs)
	clone.attachments = make([]*Attachment, len(data.attachments))
	copy(clone.attachments, data.attachments)
	clone.attributes = maps.Clone(data.attributes)
	clone.contexts = maps.Clone(data.contexts)
	clone.tags = maps.Clone(data.tags)
	clone.fingerprint = make([]string, len(data.fingerprint))
	copy(clone.fingerprint, data.fingerprint)
	clone.propagationContext = data.propagationContext.clone()
	return clone
}

// Clear removes data from the scope while retaining event processors.
func (scope *Scope) Clear() {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	propagationContext := scope.propagationContext
	span := scope.span
	scope.scopeData = newScopeData()
	scope.propagationContext = propagationContext
	scope.span = span
}

// AddEventProcessor adds an event processor to the current scope.
func (scope *Scope) AddEventProcessor(processor EventProcessor) {
	scope.mu.Lock()
	defer scope.mu.Unlock()

	scope.eventProcessors = append(scope.eventProcessors, processor)
}

// captureState retains only data that must be used after Scope locks are
// released. Maps and replacement fields are applied directly to the event.
type captureState struct {
	level       Level
	request     *http.Request
	requestBody interface {
		Bytes() []byte
		Overflow() bool
	}
	breadcrumbs []*Breadcrumb
	attachments []*Attachment
	processors  []EventProcessor
}

// resolveCaptureState applies the operation Scope first so it wins, then fills
// gaps from GlobalScope. Each Scope is read under one short lock.
func resolveCaptureState(event *Event, scope *Scope) captureState {
	var state captureState
	global := GlobalScope()
	if scope != nil && scope != global {
		state.mergeScope(event, scope, false)
	}
	state.mergeScope(event, global, true)
	return state
}

func (state *captureState) mergeScope(event *Event, scope *Scope, prepend bool) {
	scope.mu.RLock()
	defer scope.mu.RUnlock()

	if event.User.IsEmpty() && !scope.user.IsEmpty() {
		event.User = scope.user
	}
	event.Tags = util.FillMap(event.Tags, scope.tags)
	for key, value := range scope.contexts {
		if key == "trace" && event.Type == transactionType {
			continue
		}
		if event.Contexts == nil {
			event.Contexts = make(map[string]Context, len(scope.contexts))
		}
		if _, ok := event.Contexts[key]; !ok {
			event.Contexts[key] = cloneContext(value)
		}
	}
	if len(event.Fingerprint) == 0 && len(scope.fingerprint) > 0 {
		event.Fingerprint = append(event.Fingerprint, scope.fingerprint...)
	}
	if state.level == "" {
		state.level = scope.level
	}
	if state.request == nil && scope.request != nil {
		state.request = scope.request
		state.requestBody = scope.requestBody
	}

	if prepend {
		state.breadcrumbs = prependSlice(scope.breadcrumbs, state.breadcrumbs)
		state.attachments = prependSlice(scope.attachments, state.attachments)
		state.processors = prependSlice(scope.eventProcessors, state.processors)
	} else {
		state.breadcrumbs = append(state.breadcrumbs, scope.breadcrumbs...)
		state.attachments = append(state.attachments, scope.attachments...)
		state.processors = append(state.processors, scope.eventProcessors...)
	}
}

func prependSlice[T any](prefix, values []T) []T {
	if len(prefix) == 0 {
		return values
	}
	result := make([]T, 0, len(prefix)+len(values))
	result = append(result, prefix...)
	return append(result, values...)
}

func setAttributeIfAbsent(attrs map[string]attribute.Value, key, value string) {
	if value == "" {
		return
	}
	if _, ok := attrs[key]; !ok {
		attrs[key] = attribute.StringValue(value)
	}
}

func copySignalAttributes(specific, instance map[string]attribute.Value) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value, len(specific)+len(instance)+8)
	attrs = util.FillMap(attrs, specific)
	return util.FillMap(attrs, instance)
}

func buildMetricAttributes(specific []attribute.Builder, instance map[string]attribute.Value) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value, len(specific)+len(instance)+8)
	for _, attr := range specific {
		attrs[attr.Key] = attr.Value
	}
	return util.FillMap(attrs, instance)
}

func mergeScopeAttributes(attrs, defaults map[string]attribute.Value, scope *Scope) map[string]attribute.Value {
	global := GlobalScope()
	var user User
	if scope != nil && scope != global {
		scope.mu.RLock()
		attrs = util.FillMap(attrs, scope.attributes)
		user = scope.user
		scope.mu.RUnlock()
	}

	global.mu.RLock()
	attrs = util.FillMap(attrs, global.attributes)
	if user.IsEmpty() {
		user = global.user
	}
	global.mu.RUnlock()

	setAttributeIfAbsent(attrs, "user.id", user.ID)
	setAttributeIfAbsent(attrs, "user.name", user.Name)
	setAttributeIfAbsent(attrs, "user.email", user.Email)
	return util.FillMap(attrs, defaults)
}

// applyToEvent handles capture work that must run after Scope locks are
// released, including callbacks and request conversion.
func (state captureState) applyToEvent(event *Event, hint *EventHint, client *Client, opts captureOptions) *Event {
	event.Attachments = append(event.Attachments, state.attachments...)

	if event.Request == nil && state.request != nil {
		event.Request = newRequest(state.request, client)
		// NOTE: The SDK does not attempt to send partial request body data.
		//
		// The reason being that Sentry's ingest pipeline and UI are optimized
		// to show structured data. Additionally, tooling around PII scrubbing
		// relies on structured data; truncated request bodies would create
		// invalid payloads that are more prone to leaking PII data.
		//
		// Users can still send more data along their events if they want to,
		// for example using Event.Contexts.
		dc := client.GetDataCollection()
		body := state.requestBody
		if body != nil && !body.Overflow() && dc.CollectHTTPBody(BodyIncomingRequest) {
			event.Request.Data = dc.FilterHTTPBody(body.Bytes(), state.request.Header.Get("Content-Type"))
		}
	}

	event.Breadcrumbs = mergeBreadcrumbs(client.options.MaxBreadcrumbs, event.Breadcrumbs, [][]*Breadcrumb{state.breadcrumbs})
	event.Level = resolveLevel(event, state.level, opts)

	for _, processor := range state.processors {
		id := event.EventID
		category := event.toCategory()
		spanCountBefore := event.GetSpanCount()
		event = processor(event, hint)
		if event == nil {
			debuglog.Printf("Event dropped by one of the Scope EventProcessors: %s\n", id)
			client.recordDiscard(report.ReasonEventProcessor, category, 1)
			if category == ratelimit.CategoryTransaction {
				client.recordDiscard(report.ReasonEventProcessor, ratelimit.CategorySpan, int64(spanCountBefore))
			}
			return nil
		}
		if droppedSpans := spanCountBefore - event.GetSpanCount(); droppedSpans > 0 {
			client.recordDiscard(report.ReasonEventProcessor, ratelimit.CategorySpan, int64(droppedSpans))
		}
	}

	return event
}

func resolveLevel(event *Event, scopeLevel Level, opts captureOptions) Level {
	switch {
	case event.Level != "":
		return event.Level
	case opts.level != "":
		return opts.level
	case scopeLevel != "" && event.Type != transactionType:
		return scopeLevel
	case opts.defaultLevel != "":
		return opts.defaultLevel
	default:
		return LevelInfo
	}
}

func mergeBreadcrumbs(limit int, own []*Breadcrumb, layers [][]*Breadcrumb) []*Breadcrumb {
	if limit < 0 {
		return nil
	}
	if limit == 0 {
		limit = defaultMaxBreadcrumbs
	}

	total := len(own)
	for _, layer := range layers {
		total += len(layer)
	}
	if total == 0 {
		return own
	}
	merged := make([]*Breadcrumb, 0, total)
	merged = append(merged, own...)
	for _, layer := range layers {
		merged = append(merged, layer...)
	}
	if len(merged) > limit {
		merged = merged[len(merged)-limit:]
	}
	return merged
}

// cloneContext returns a new context with keys and values copied from the passed one.
//
// Note: a new Context (map) is returned, but the function does NOT do
// a proper deep copy: if some context values are pointer types (e.g. maps),
// they won't be properly copied.
func cloneContext(c Context) Context {
	res := make(Context, len(c))
	for k, v := range c {
		res[k] = v
	}
	return res
}

type signalCaptureContext struct {
	scope             *Scope
	ctx               context.Context
	defaultAttributes map[string]attribute.Value
}

// resolveTrace resolves trace IDs and dynamic sampling context from the given
// contexts and scope. It is the single trace-policy function used by every
// signal. The precedence is external resolver, span in a supplied context,
// scope span, then scope propagation context.
//
// TODO: this should be removed when the span API is introduced. Currently kept for compatibility. The span
// and trace should only be resolved through context.
func resolveTrace(scope *Scope, client *Client, ctxs ...context.Context) traceResolution {
	client = normalizeClient(client)
	var (
		resolved traceResolution
		span     *Span
		external bool
	)

	for _, ctx := range ctxs {
		if ctx == nil {
			continue
		}
		if traceID, spanID, ok := client.externalTraceContextFromContext(ctx); ok {
			resolved.traceID, resolved.spanID, resolved.telemetrySpanID = traceID, spanID, spanID
			resolved.valid, resolved.external, external = true, true, true
			break
		}
		if span = SpanFromContext(ctx); span != nil {
			break
		}
	}

	if scope == nil {
		return resolved
	}

	scope.mu.RLock()
	scopeSpan := scope.span
	propagation := scope.propagationContext
	request := scope.request
	scope.mu.RUnlock()

	// The request context is a fallback caller context.
	if !external && span == nil && request != nil {
		if traceID, spanID, ok := client.externalTraceContextFromContext(request.Context()); ok {
			resolved.traceID, resolved.spanID, resolved.telemetrySpanID = traceID, spanID, spanID
			resolved.valid, resolved.external, external = true, true, true
		}
	}
	if span == nil && !external {
		span = scopeSpan
	}

	if !external {
		if span != nil {
			resolved.traceID, resolved.spanID, resolved.telemetrySpanID = span.TraceID, span.SpanID, span.SpanID
			resolved.eventContext = span.traceContext().Map()
			resolved.valid = true
		} else {
			resolved.traceID, resolved.spanID = propagation.TraceID, propagation.SpanID
			resolved.valid = propagation.TraceID != (TraceID{})
		}
	}

	if span != nil {
		if transaction := span.GetTransaction(); transaction != nil {
			resolved.dsc = DynamicSamplingContextFromTransaction(transaction)
			return resolved
		}
	}
	resolved.dsc = propagation.DynamicSamplingContext
	if !resolved.dsc.HasEntries() {
		// DynamicSamplingContextFromScope only reads propagationContext. Re-read
		// under its documented locking contract rather than duplicating its
		// client-option/DSN logic here.
		resolved.dsc = dynamicSamplingContextFromScope(scope, client)
	}

	return resolved
}

type traceResolution struct {
	traceID         TraceID
	spanID          SpanID  // event projection, including propagation SpanID
	telemetrySpanID SpanID  // only an active/external span belongs on logs/metrics
	eventContext    Context // full local span context for error event compatibility
	dsc             DynamicSamplingContext
	valid           bool
	external        bool
}

func applyTraceToEvent(event *Event, trace traceResolution) {
	if event.Type == transactionType || !trace.valid {
		return
	}
	if event.Contexts == nil {
		event.Contexts = make(map[string]Context)
	}
	if _, ok := event.Contexts["trace"]; !ok {
		if trace.eventContext != nil {
			event.Contexts["trace"] = trace.eventContext
		} else {
			traceID, spanID := any(trace.traceID), any(trace.spanID)
			if trace.external {
				traceID, spanID = trace.traceID.String(), trace.spanID.String()
			}
			event.Contexts["trace"] = Context{"trace_id": traceID, "span_id": spanID}
		}
	}
	event.sdkMetaData.dsc = trace.dsc
}
