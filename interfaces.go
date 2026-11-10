package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
)

const transactionType = "transaction"

// Level marks the severity of the event.
type Level string

// Describes the severity of the event.
const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
	LevelFatal   Level = "fatal"
)

// SdkInfo contains all metadata about the SDK.
type SdkInfo = protocol.SdkInfo
type SdkPackage = protocol.SdkPackage

// TODO: This type could be more useful, as map of interface{} is too generic
// and requires a lot of type assertions in beforeBreadcrumb calls
// plus it could just be map[string]interface{} then.

// BreadcrumbHint contains information that can be associated with a Breadcrumb.
type BreadcrumbHint map[string]any

// Breadcrumb specifies an application event that occurred before a Sentry event.
// An event may contain one or more breadcrumbs.
type Breadcrumb struct {
	Type      string         `json:"type,omitempty"`
	Category  string         `json:"category,omitempty"`
	Message   string         `json:"message,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Level     Level          `json:"level,omitempty"`
	Timestamp time.Time      `json:"timestamp,omitzero"`
}

// TODO: provide constants for known breadcrumb types.
// See https://develop.sentry.dev/sdk/event-payloads/breadcrumbs/#breadcrumb-types.

// Logger provides a chaining API for structured logging to Sentry.
type Logger interface {
	// Write implements the io.Writer interface using the context passed to NewLogger.
	Write(p []byte) (n int, err error)

	// SetAttributes allows attaching parameters to the logger using the attribute API.
	// These attributes will be included in all subsequent log entries.
	SetAttributes(...attribute.Builder)

	// Trace defines the [sentry.LogLevel] for the log entry.
	Trace() LogEntry
	// Debug defines the [sentry.LogLevel] for the log entry.
	Debug() LogEntry
	// Info defines the [sentry.LogLevel] for the log entry.
	Info() LogEntry
	// Warn defines the [sentry.LogLevel] for the log entry.
	Warn() LogEntry
	// Error defines the [sentry.LogLevel] for the log entry.
	Error() LogEntry
	// Fatal defines the [sentry.LogLevel] for the log entry.
	Fatal() LogEntry
	// Panic defines the [sentry.LogLevel] for the log entry.
	Panic() LogEntry
	// LFatal defines the [sentry.LogLevel] for the log entry. This only sets
	// the level to fatal, but does not panic or exit.
	LFatal() LogEntry
	// GetCtx returns the [context.Context] set on the logger.
	GetCtx() context.Context
}

// LogEntry defines the interface for a log entry that supports chaining attributes.
type LogEntry interface {
	// WithCtx creates a new LogEntry with the specified context without overwriting the previous one.
	WithCtx(ctx context.Context) LogEntry
	// StringSlice adds a string slice attribute to the LogEntry.
	StringSlice(key string, value []string) LogEntry
	// String adds a string attribute to the LogEntry.
	String(key, value string) LogEntry
	// Int adds an int attribute to the LogEntry.
	Int(key string, value int) LogEntry
	// Int64Slice adds an int64 slice attribute to the LogEntry.
	Int64Slice(key string, value []int64) LogEntry
	// Int64 adds an int64 attribute to the LogEntry.
	Int64(key string, value int64) LogEntry
	// Float64Slice adds a float64 slice attribute to the LogEntry.
	Float64Slice(key string, value []float64) LogEntry
	// Float64 adds a float64 attribute to the LogEntry.
	Float64(key string, value float64) LogEntry
	// BoolSlice adds a bool slice attribute to the LogEntry.
	BoolSlice(key string, value []bool) LogEntry
	// Bool adds a bool attribute to the LogEntry.
	Bool(key string, value bool) LogEntry
	// Emit emits the LogEntry with the provided arguments.
	Emit(args ...any)
	// Emitf emits the LogEntry using a format string and arguments.
	Emitf(format string, args ...any)
}

// Meter provides an interface for recording metrics.
type Meter interface {
	// WithCtx returns a new Meter that uses the given context for trace/span association.
	WithCtx(ctx context.Context) Meter
	// SetAttributes allows attaching parameters to the meter using the attribute API.
	// These attributes will be included in all subsequent metrics.
	SetAttributes(attrs ...attribute.Builder)
	// Count records a count metric.
	Count(name string, count int64, opts ...MeterOption)
	// Gauge records a gauge metric.
	Gauge(name string, value float64, opts ...MeterOption)
	// Distribution records a distribution metric.
	Distribution(name string, sample float64, opts ...MeterOption)
}

// MeterOption configures a metric recording call.
type MeterOption func(*meterOptions)

type meterOptions struct {
	unit       string
	attributes map[string]attribute.Value
}

// WithUnit sets the unit for the metric (e.g., "millisecond", "byte").
func WithUnit(unit string) MeterOption {
	return func(o *meterOptions) {
		o.unit = unit
	}
}

// WithAttributes sets attributes for the metric.
func WithAttributes(attrs ...attribute.Builder) MeterOption {
	return func(o *meterOptions) {
		if o.attributes == nil {
			o.attributes = make(map[string]attribute.Value, len(attrs))
		}
		for _, a := range attrs {
			if a.Value.Type() == attribute.INVALID {
				debuglog.Printf("invalid attribute: %v", a)
				continue
			}
			o.attributes[a.Key] = a.Value
		}
	}
}

// Attachment allows associating files with your events to aid in investigation.
// An event may contain one or more attachments.
type Attachment struct {
	Filename    string
	ContentType string
	Payload     []byte
}

// User describes the user associated with an Event. If this is used, at least
// an ID or an IP address should be provided.
type User struct {
	ID        string            `json:"id,omitempty"`
	Email     string            `json:"email,omitempty"`
	IPAddress string            `json:"ip_address,omitempty"`
	Username  string            `json:"username,omitempty"`
	Name      string            `json:"name,omitempty"`
	Data      map[string]string `json:"data,omitempty"`
}

func (u User) IsEmpty() bool {
	if u.ID != "" {
		return false
	}

	if u.Email != "" {
		return false
	}

	if u.IPAddress != "" {
		return false
	}

	if u.Username != "" {
		return false
	}

	if u.Name != "" {
		return false
	}

	if len(u.Data) > 0 {
		return false
	}

	return true
}

// Request contains information on a HTTP request related to the event.
type Request struct {
	URL         string            `json:"url,omitempty"`
	Method      string            `json:"method,omitempty"`
	Data        string            `json:"data,omitempty"`
	QueryString string            `json:"query_string,omitempty"`
	Cookies     string            `json:"cookies,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

func newRequest(r *http.Request, client *Client) *Request {
	prot := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		prot = "https"
	}
	url := fmt.Sprintf("%s://%s%s", prot, r.Host, r.URL.Path)

	dc := client.GetDataCollection()

	var cookies string
	headers := make(map[string]string, len(r.Header)+1)
	if dc.CollectCookies() {
		cookies = dc.FilterCookies(r.Header.Values("Cookie"))
		if cookies != "" {
			headers["Cookie"] = cookies
		}
	}
	for k, v := range r.Header {
		if strings.EqualFold(k, "Cookie") {
			continue
		}
		headers[k] = strings.Join(v, ",")
	}
	headers["Host"] = r.Host
	headers = dc.FilterRequestHeaders(headers)

	var env map[string]string
	if dc.UserInfo.Value {
		if addr, port, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			env = map[string]string{"REMOTE_ADDR": addr, "REMOTE_PORT": port}
		}
	}

	return &Request{
		URL:         url,
		Method:      r.Method,
		QueryString: dc.FilterQueryString(r.URL.RawQuery),
		Cookies:     cookies,
		Headers:     headers,
		Env:         env,
	}
}

// NewRequest returns a new Sentry Request from the given http.Request.
//
// NewRequest avoids operations that depend on network access. In particular, it
// does not read r.Body.
func NewRequest(r *http.Request) *Request {
	return newRequest(r, ClientFromContext(r.Context()))
}

// Mechanism is the mechanism by which an exception was generated and handled.
type Mechanism struct {
	Type             string         `json:"type"`
	Description      string         `json:"description,omitempty"`
	HelpLink         string         `json:"help_link,omitempty"`
	Source           string         `json:"source,omitempty"`
	Handled          *bool          `json:"handled,omitempty"`
	ParentID         *int           `json:"parent_id,omitempty"`
	ExceptionID      int            `json:"exception_id"`
	IsExceptionGroup bool           `json:"is_exception_group,omitempty"`
	Data             map[string]any `json:"data,omitempty"`
}

// SetUnhandled indicates that the exception is an unhandled exception, i.e.
// from a panic.
func (m *Mechanism) SetUnhandled() {
	m.Handled = new(false)
}

// Exception specifies an error that occurred.
type Exception struct {
	Type       string      `json:"type,omitempty"`  // used as the main issue title
	Value      string      `json:"value,omitempty"` // used as the main issue subtitle
	Module     string      `json:"module,omitempty"`
	ThreadID   uint64      `json:"thread_id,omitempty"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Mechanism  *Mechanism  `json:"mechanism,omitempty"`
}

// SDKMetaData is a struct to stash data which is needed at some point in the SDK's event processing pipeline
// but which shouldn't get send to Sentry.
type SDKMetaData struct {
	dsc DynamicSamplingContext
}

// Contains information about how the name of the transaction was determined.
type TransactionInfo struct {
	Source TransactionSource `json:"source,omitempty"`
}

// The DebugMeta interface is not used in Golang apps, but may be populated
// when proxying Events from other platforms, like iOS, Android, and the
// Web.  (See: https://develop.sentry.dev/sdk/event-payloads/debugmeta/ ).
type DebugMeta struct {
	SdkInfo *DebugMetaSdkInfo `json:"sdk_info,omitempty"`
	Images  []DebugMetaImage  `json:"images,omitempty"`
}

type DebugMetaSdkInfo struct {
	SdkName           string `json:"sdk_name,omitempty"`
	VersionMajor      int    `json:"version_major,omitempty"`
	VersionMinor      int    `json:"version_minor,omitempty"`
	VersionPatchlevel int    `json:"version_patchlevel,omitempty"`
}

type DebugMetaImage struct {
	Type        string `json:"type,omitempty"`         // all
	ImageAddr   string `json:"image_addr,omitempty"`   // macho,elf,pe
	ImageSize   int    `json:"image_size,omitempty"`   // macho,elf,pe
	DebugID     string `json:"debug_id,omitempty"`     // macho,elf,pe,wasm,sourcemap
	DebugFile   string `json:"debug_file,omitempty"`   // macho,elf,pe,wasm
	CodeID      string `json:"code_id,omitempty"`      // macho,elf,pe,wasm
	CodeFile    string `json:"code_file,omitempty"`    // macho,elf,pe,wasm,sourcemap
	ImageVmaddr string `json:"image_vmaddr,omitempty"` // macho,elf,pe
	Arch        string `json:"arch,omitempty"`         // macho,elf,pe
	UUID        string `json:"uuid,omitempty"`         // proguard
}

// EventID is a hexadecimal string representing a unique uuid4 for an Event.
// An EventID must be 32 characters long, lowercase and not have any dashes.
type EventID string

type Context = map[string]any

// Event is the fundamental data structure that is sent to Sentry.
type Event struct {
	Breadcrumbs []*Breadcrumb      `json:"breadcrumbs,omitempty"`
	Contexts    map[string]Context `json:"contexts,omitempty"`
	Dist        string             `json:"dist,omitempty"`
	Environment string             `json:"environment,omitempty"`
	EventID     EventID            `json:"event_id,omitempty"`
	Fingerprint []string           `json:"fingerprint,omitempty"`
	Level       Level              `json:"level,omitempty"`
	Message     string             `json:"message,omitempty"`
	Platform    string             `json:"platform,omitempty"`
	Release     string             `json:"release,omitempty"`
	Sdk         SdkInfo            `json:"sdk"`
	ServerName  string             `json:"server_name,omitempty"`
	Threads     []Thread           `json:"threads,omitempty"`
	Tags        map[string]string  `json:"tags,omitempty"`
	Timestamp   time.Time          `json:"timestamp,omitzero"`
	Transaction string             `json:"transaction,omitempty"`
	User        User               `json:"user,omitzero"`
	Logger      string             `json:"logger,omitempty"`
	Modules     map[string]string  `json:"modules,omitempty"`
	Request     *Request           `json:"request,omitempty"`
	Exception   []Exception        `json:"exception,omitempty"`
	DebugMeta   *DebugMeta         `json:"debug_meta,omitempty"`
	Attachments []*Attachment      `json:"-"`

	// The fields below are not part of the final JSON payload.

	sdkMetaData SDKMetaData
}

// SetException appends the unwrapped errors to the event's exception list.
//
// maxErrorDepth is the maximum depth of the error chain we will look
// into while unwrapping the errors. If maxErrorDepth is -1, we will
// unwrap all errors in the chain.
func (e *Event) SetException(exception error, maxErrorDepth int) {
	if exception == nil {
		return
	}

	exceptions := convertErrorToExceptions(exception, maxErrorDepth)
	if len(exceptions) == 0 {
		return
	}

	e.Exception = exceptions
}

// eventItem is an event as handed to the telemetry processor.
type eventItem Event

func (e *eventItem) GetCategory() ratelimit.Category { return ratelimit.CategoryError }

// ToEnvelope encodes the event and its attachments.
func (e *eventItem) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	item, err := encodeItem(protocol.EnvelopeItemTypeEvent, (*Event)(e))
	if err != nil {
		return nil, err
	}
	envelope := protocol.NewEnvelope(header, item)
	for _, attachment := range e.Attachments {
		envelope.AddItem(protocol.NewAttachmentItem(attachment.Filename, attachment.ContentType, attachment.Payload))
	}
	return envelope, nil
}

// GetEventID returns the event ID.
func (e *eventItem) GetEventID() string {
	return string(e.EventID)
}

// GetSdkInfo returns SDK information for the envelope header.
func (e *eventItem) GetSdkInfo() *protocol.SdkInfo {
	return &e.Sdk
}

// GetDynamicSamplingContext returns trace context for the envelope header.
func (e *eventItem) GetDynamicSamplingContext() map[string]string {
	return e.sdkMetaData.dsc.Entries
}

// encodeItem encodes payload as an envelope item.
func encodeItem(itemType protocol.EnvelopeItemType, payload any) (item *protocol.EnvelopeItem, err error) {
	defer func() {
		if r := recover(); r != nil {
			item, err = nil, fmt.Errorf("panic during event marshaling: %v", r)
		}
	}()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("could not encode event as JSON, skipping delivery: %w", err)
	}
	return protocol.NewEnvelopeItem(itemType, encoded), nil
}

// TODO: Event.Contexts map[string]interface{} => map[string]EventContext,
// to prevent accidentally storing T when we mean *T.
// For example, the TraceContext must be stored as *TraceContext to pick up the
// MarshalJSON method (and avoid copying).
// type EventContext interface{ EventContext() }

// NewEvent creates a new Event.
func NewEvent() *Event {
	return &Event{
		Contexts: make(map[string]Context),
		Tags:     make(map[string]string),
		Modules:  make(map[string]string),
	}
}

// Transaction is a finished root span together with its finished child spans.
type Transaction struct {
	EventID         EventID          `json:"event_id,omitempty"`
	Name            string           `json:"transaction,omitempty"`
	TransactionInfo *TransactionInfo `json:"transaction_info,omitempty"`
	StartTime       time.Time        `json:"start_timestamp,omitzero"`
	EndTime         time.Time        `json:"timestamp,omitzero"`
	// Trace describes the root span. It is sent as the trace context.
	Trace       TraceContext       `json:"-"`
	Spans       []*Span            `json:"spans,omitempty"`
	Tags        map[string]string  `json:"tags,omitempty"`
	Contexts    map[string]Context `json:"contexts,omitempty"`
	Breadcrumbs []*Breadcrumb      `json:"breadcrumbs,omitempty"`
	User        User               `json:"user,omitzero"`
	Request     *Request           `json:"request,omitempty"`
	Release     string             `json:"release,omitempty"`
	Dist        string             `json:"dist,omitempty"`
	Environment string             `json:"environment,omitempty"`
	ServerName  string             `json:"server_name,omitempty"`
	Platform    string             `json:"platform,omitempty"`
	Sdk         SdkInfo            `json:"sdk"`
}

// transactionItem is a transaction as handed to the telemetry processor.
type transactionItem struct {
	Transaction
	Type string `json:"type"`
	// dsc is the envelope's trace header.
	dsc map[string]string
}

// newTransactionItem returns an item that owns a copy of tx.
func newTransactionItem(tx *Transaction, dsc map[string]string) *transactionItem {
	item := &transactionItem{Transaction: tx.owned(), Type: transactionType, dsc: maps.Clone(dsc)}
	if item.Contexts == nil {
		item.Contexts = make(map[string]Context, 1)
	}
	item.Contexts[traceContextKey] = item.Trace.Map()
	return item
}

func (t *transactionItem) GetCategory() ratelimit.Category              { return ratelimit.CategoryTransaction }
func (t *transactionItem) GetEventID() string                           { return string(t.EventID) }
func (t *transactionItem) GetSdkInfo() *protocol.SdkInfo                { return &t.Sdk }
func (t *transactionItem) GetDynamicSamplingContext() map[string]string { return t.dsc }

// ToEnvelope encodes the transaction.
func (t *transactionItem) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	item, err := encodeItem(protocol.EnvelopeItemTypeTransaction, t)
	if err != nil {
		return nil, err
	}
	return protocol.NewEnvelope(header, item), nil
}

// Thread specifies threads that were running at the time of an event.
type Thread struct {
	ID         string      `json:"id,omitempty"`
	Name       string      `json:"name,omitempty"`
	Stacktrace *Stacktrace `json:"stacktrace,omitempty"`
	Crashed    bool        `json:"crashed,omitempty"`
	Current    bool        `json:"current,omitempty"`
}

// EventHint contains information that can be associated with an Event.
type EventHint struct {
	Data               any
	EventID            string
	OriginalException  error
	RecoveredException any
	Context            context.Context
	Request            *http.Request
	Response           *http.Response
}

type Log struct {
	Timestamp  time.Time                  `json:"timestamp,omitzero"`
	TraceID    TraceID                    `json:"trace_id"`
	SpanID     SpanID                     `json:"span_id,omitzero"`
	Level      LogLevel                   `json:"level"`
	Severity   int                        `json:"severity_number,omitempty"`
	Body       string                     `json:"body"`
	Attributes map[string]attribute.Value `json:"attributes,omitempty"`
}

// logItem is a Log as handed to the telemetry processor.
type logItem Log

func (l *logItem) GetCategory() ratelimit.Category { return ratelimit.CategoryLog }

type MetricType string

const (
	MetricTypeInvalid      MetricType = ""
	MetricTypeCounter      MetricType = "counter"
	MetricTypeGauge        MetricType = "gauge"
	MetricTypeDistribution MetricType = "distribution"
)

type Metric struct {
	Timestamp  time.Time                  `json:"timestamp,omitzero"`
	TraceID    TraceID                    `json:"trace_id"`
	SpanID     SpanID                     `json:"span_id,omitzero"`
	Type       MetricType                 `json:"type"`
	Name       string                     `json:"name"`
	Value      MetricValue                `json:"value"`
	Unit       string                     `json:"unit,omitempty"`
	Attributes map[string]attribute.Value `json:"attributes,omitempty"`
}

// metricItem is a Metric as handed to the telemetry processor.
type metricItem Metric

func (m *metricItem) GetCategory() ratelimit.Category { return ratelimit.CategoryTraceMetric }

// MetricValue stores metric values with full precision.
// It supports int64 (for counters) and float64 (for gauges and distributions).
type MetricValue struct {
	value attribute.Value
}

// Int64MetricValue creates a MetricValue from an int64.
// Used for counter metrics to preserve full int64 precision.
func Int64MetricValue(v int64) MetricValue {
	return MetricValue{value: attribute.Int64Value(v)}
}

// Float64MetricValue creates a MetricValue from a float64.
// Used for gauge and distribution metrics.
func Float64MetricValue(v float64) MetricValue {
	return MetricValue{value: attribute.Float64Value(v)}
}

// Type returns the type of the stored value (attribute.INT64 or attribute.FLOAT64).
func (v MetricValue) Type() attribute.Type {
	return v.value.Type()
}

// Int64 returns the value as int64 if it holds an int64.
// The second return value indicates whether the type matched.
func (v MetricValue) Int64() (int64, bool) {
	if v.value.Type() == attribute.INT64 {
		return v.value.AsInt64(), true
	}
	return 0, false
}

// Float64 returns the value as float64 if it holds a float64.
// The second return value indicates whether the type matched.
func (v MetricValue) Float64() (float64, bool) {
	if v.value.Type() == attribute.FLOAT64 {
		return v.value.AsFloat64(), true
	}
	return 0, false
}

// AsInterface returns the value as int64 or float64.
// Use type assertion or type switch to handle the result.
func (v MetricValue) AsInterface() any {
	return v.value.AsInterface()
}

// MarshalJSON serializes the value as a bare number.
func (v MetricValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.value.AsInterface())
}

// owned returns a copy of e that shares no memory its caller can still write,
// except attachment payloads.
func (e *Event) owned() Event {
	owned := *e
	owned.Breadcrumbs = ownBreadcrumbs(e.Breadcrumbs)
	owned.Contexts = ownContexts(e.Contexts)
	owned.Fingerprint = slices.Clone(e.Fingerprint)
	owned.Sdk = ownSdk(e.Sdk)
	owned.Threads = slices.Clone(e.Threads)
	for i := range owned.Threads {
		owned.Threads[i].Stacktrace = owned.Threads[i].Stacktrace.owned()
	}
	owned.Tags = maps.Clone(e.Tags)
	owned.User.Data = maps.Clone(e.User.Data)
	owned.Modules = maps.Clone(e.Modules)
	owned.Request = ownRequest(e.Request)
	owned.Exception = slices.Clone(e.Exception)
	for i := range owned.Exception {
		exception := &owned.Exception[i]
		exception.Stacktrace = exception.Stacktrace.owned()
		if exception.Mechanism = copyOf(exception.Mechanism); exception.Mechanism != nil {
			exception.Mechanism.Data = ownMap(exception.Mechanism.Data)
			exception.Mechanism.Handled = copyOf(exception.Mechanism.Handled)
			exception.Mechanism.ParentID = copyOf(exception.Mechanism.ParentID)
		}
	}
	if owned.DebugMeta = copyOf(e.DebugMeta); owned.DebugMeta != nil {
		owned.DebugMeta.SdkInfo = copyOf(owned.DebugMeta.SdkInfo)
		owned.DebugMeta.Images = slices.Clone(owned.DebugMeta.Images)
	}
	owned.Attachments = copyEach(e.Attachments)
	owned.sdkMetaData.dsc.Entries = maps.Clone(e.sdkMetaData.dsc.Entries)
	return owned
}

// owned returns a copy of t that shares no memory its caller can still write.
func (t *Transaction) owned() Transaction {
	owned := *t
	owned.TransactionInfo = copyOf(t.TransactionInfo)
	owned.Trace.Data = ownMap(t.Trace.Data)
	owned.Spans = make([]*Span, 0, len(t.Spans))
	for _, span := range t.Spans {
		if span != nil {
			owned.Spans = append(owned.Spans, span.detached())
		}
	}
	owned.Tags = maps.Clone(t.Tags)
	owned.Contexts = ownContexts(t.Contexts)
	owned.Breadcrumbs = ownBreadcrumbs(t.Breadcrumbs)
	owned.User.Data = maps.Clone(t.User.Data)
	owned.Request = ownRequest(t.Request)
	owned.Sdk = ownSdk(t.Sdk)
	return owned
}

func ownBreadcrumbs(breadcrumbs []*Breadcrumb) []*Breadcrumb {
	breadcrumbs = copyEach(breadcrumbs)
	for _, breadcrumb := range breadcrumbs {
		breadcrumb.Data = ownMap(breadcrumb.Data)
	}
	return breadcrumbs
}

func ownContexts(contexts map[string]Context) map[string]Context {
	if contexts == nil {
		return nil
	}
	owned := make(map[string]Context, len(contexts))
	for name, context := range contexts {
		owned[name] = ownMap(context)
	}
	return owned
}

func ownRequest(request *Request) *Request {
	if request = copyOf(request); request != nil {
		request.Headers = maps.Clone(request.Headers)
		request.Env = maps.Clone(request.Env)
	}
	return request
}

func ownSdk(sdk SdkInfo) SdkInfo {
	sdk.Integrations = slices.Clone(sdk.Integrations)
	sdk.Packages = slices.Clone(sdk.Packages)
	return sdk
}

// owned returns a copy of s that shares no memory with it.
func (s *Stacktrace) owned() *Stacktrace {
	if s == nil {
		return nil
	}
	owned := &Stacktrace{Frames: slices.Clone(s.Frames), FramesOmitted: slices.Clone(s.FramesOmitted)}
	for i := range owned.Frames {
		frame := &owned.Frames[i]
		frame.PreContext = slices.Clone(frame.PreContext)
		frame.PostContext = slices.Clone(frame.PostContext)
		frame.Vars = ownMap(frame.Vars)
	}
	return owned
}

// copyOf returns a pointer to a copy of *p, or nil.
func copyOf[T any](p *T) *T {
	if p == nil {
		return nil
	}
	c := *p
	return &c
}

// copyEach returns pointers to copies of the non-nil elements of s.
func copyEach[T any](s []*T) []*T {
	if len(s) == 0 {
		return nil
	}
	values := make([]T, 0, len(s))
	copies := make([]*T, 0, len(s))
	for _, p := range s {
		if p != nil {
			values = append(values, *p)
			copies = append(copies, &values[len(values)-1])
		}
	}
	return copies
}

// ownMap returns a copy of m whose values nothing else can write.
func ownMap(m map[string]any) map[string]any {
	m = maps.Clone(m)
	ownValues(m)
	return m
}

// ownValues replaces the values of m that something else could still write.
// It may run a value's own encoder, so no lock must be held, and m must not be shared.
func ownValues(m map[string]any) {
	for key, value := range m {
		if isScalar(value) {
			continue
		}
		if owned, ok := ownValue(value); ok {
			m[key] = owned
		} else {
			debuglog.Printf("Dropping %q: its value cannot be encoded as JSON", key)
			delete(m, key)
		}
	}
}

// ownValue returns a copy of a container of scalars, and the JSON encoding of any other value.
func ownValue(value any) (any, bool) {
	switch value := value.(type) {
	case map[string]string:
		return maps.Clone(value), true
	case []string:
		return slices.Clone(value), true
	case map[string]any:
		scalars := true
		for _, element := range value {
			scalars = scalars && isScalar(element)
		}
		if scalars {
			return maps.Clone(value), true
		}
	case []any:
		if !slices.ContainsFunc(value, func(element any) bool { return !isScalar(element) }) {
			return slices.Clone(value), true
		}
	}
	return encodeValue(value)
}

// isScalar reports whether value is immutable and always encodes as JSON.
func isScalar(value any) bool {
	switch value := value.(type) {
	case nil, bool, string,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		time.Duration, TraceID, SpanID, SpanStatus:
		return true
	case float64:
		return !math.IsNaN(value) && !math.IsInf(value, 0)
	case float32:
		return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
	}
	return false
}

func encodeValue(value any) (encoded any, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	raw, err := json.Marshal(value)
	return json.RawMessage(raw), err == nil
}
