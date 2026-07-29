package sentry

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	httpinternal "github.com/getsentry/sentry-go/internal/http"
	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/util"
	"github.com/getsentry/sentry-go/report"
)

const (
	defaultBufferSize        = 1000
	defaultTimeout           = time.Second * 30
	defaultClientReportsTick = time.Second * 30
)

// Transport is used by the Client to deliver events to remote server.
type Transport interface {
	Flush(timeout time.Duration) bool
	FlushWithContext(ctx context.Context) bool
	Configure(options ClientOptions)
	SendEvent(event *Event)
	Close()
}

func getProxyConfig(options ClientOptions) func(*http.Request) (*url.URL, error) {
	if options.HTTPSProxy != "" {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPSProxy)
		}
	}

	if options.HTTPProxy != "" {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPProxy)
		}
	}

	return http.ProxyFromEnvironment
}

func getTLSConfig(options ClientOptions) *tls.Config {
	if options.CaCerts != nil {
		// #nosec G402 -- We should be using `MinVersion: tls.VersionTLS12`,
		// 				 but we don't want to break peoples code without the major bump.
		return &tls.Config{
			RootCAs: options.CaCerts,
		}
	}

	return nil
}

// eventDebugContext returns a panic-safe summary of an event for debug logging.
// It only touches scalar fields and len() of collections, which do not trigger
// the runtime concurrent-map-access fatal nor invoke user-defined Stringers.
func eventDebugContext(event *Event) string {
	return fmt.Sprintf(
		"event=%s type=%s level=%s "+
			"breadcrumbs=%d exceptions=%d tags=%d contexts=%d attachments=%d",
		event.EventID, event.Type, event.Level,
		len(event.Breadcrumbs), len(event.Exception),
		len(event.Tags), len(event.Contexts), len(event.Attachments),
	)
}

func getRequestBodyFromEvent(event *Event) []byte {
	body, err := json.Marshal(event)
	if err != nil {
		debuglog.Printf("Could not encode event as JSON, skipping delivery. %s: %v", eventDebugContext(event), err)
		return nil
	}
	return body
}

func encodeAttachment(enc *json.Encoder, b io.Writer, attachment *Attachment) error {
	// Attachment header
	err := enc.Encode(struct {
		Type        string `json:"type"`
		Length      int    `json:"length"`
		Filename    string `json:"filename"`
		ContentType string `json:"content_type,omitempty"`
	}{
		Type:        "attachment",
		Length:      len(attachment.Payload),
		Filename:    attachment.Filename,
		ContentType: attachment.ContentType,
	})
	if err != nil {
		return err
	}

	// Attachment payload
	if _, err = b.Write(attachment.Payload); err != nil {
		return err
	}

	// "Envelopes should be terminated with a trailing newline."
	//
	// [1]: https://develop.sentry.dev/sdk/envelopes/#envelopes
	if _, err := b.Write([]byte("\n")); err != nil {
		return err
	}

	return nil
}

func encodeClientReport(enc *json.Encoder, cr *report.ClientReport) error {
	payload, err := json.Marshal(cr)
	if err != nil {
		return err
	}
	err = encodeEnvelopeItem(enc, string(protocol.EnvelopeItemTypeClientReport), payload)
	if err != nil {
		return err
	}

	return nil
}

func encodeEnvelopeItem(enc *json.Encoder, itemType string, body json.RawMessage) error {
	// Item header
	err := enc.Encode(struct {
		Type   string `json:"type"`
		Length int    `json:"length"`
	}{
		Type:   itemType,
		Length: len(body),
	})
	if err == nil {
		// payload
		err = enc.Encode(body)
	}
	return err
}

func encodeEnvelopeLogs(enc *json.Encoder, count int, body json.RawMessage) error {
	err := enc.Encode(
		struct {
			Type        string `json:"type"`
			ItemCount   int    `json:"item_count"`
			ContentType string `json:"content_type"`
		}{
			Type:        logEvent.Type,
			ItemCount:   count,
			ContentType: logEvent.ContentType,
		})
	if err == nil {
		err = enc.Encode(body)
	}
	return err
}

func encodeEnvelopeMetrics(enc *json.Encoder, count int, body json.RawMessage) error {
	err := enc.Encode(
		struct {
			Type        string `json:"type"`
			ItemCount   int    `json:"item_count"`
			ContentType string `json:"content_type"`
		}{
			Type:        traceMetricEvent.Type,
			ItemCount:   count,
			ContentType: traceMetricEvent.ContentType,
		})
	if err == nil {
		err = enc.Encode(body)
	}
	return err
}

func recordForEvent(recorder report.ClientReportRecorder, reason report.DiscardReason, event *Event) {
	category := event.toCategory()
	switch event.Type {
	case transactionType:
		recorder.RecordOne(reason, category)
		if count := event.GetSpanCount(); count > 0 {
			recorder.Record(reason, ratelimit.CategorySpan, int64(count))
		}
	case logEvent.Type:
		if count := len(event.Logs); count > 0 {
			recorder.Record(reason, ratelimit.CategoryLog, int64(count))
		}
		if size := event.GetLogByteSize(); size > 0 {
			recorder.Record(reason, ratelimit.CategoryLogByte, int64(size))
		}
	default:
		recorder.RecordOne(reason, category)
	}
}

func recordForBatchItem(recorder report.ClientReportRecorder, reason report.DiscardReason, item *batchItem) {
	switch {
	case item.logItemCount > 0:
		recorder.Record(reason, ratelimit.CategoryLog, int64(item.logItemCount))
		if item.logByteSize > 0 {
			recorder.Record(reason, ratelimit.CategoryLogByte, int64(item.logByteSize))
		}
	default:
		recorder.RecordOne(reason, item.category)
		if item.spanCount > 0 {
			recorder.Record(reason, ratelimit.CategorySpan, int64(item.spanCount))
		}
	}
}

// envelopeHeader represents the header of a Sentry envelope.
type envelopeHeader struct {
	EventID EventID           `json:"event_id,omitempty"`
	SentAt  time.Time         `json:"sent_at"`
	Dsn     *Dsn              `json:"dsn,omitempty"`
	Sdk     map[string]string `json:"sdk,omitempty"`
	Trace   map[string]string `json:"trace,omitempty"`
}

func encodeEnvelopeHeader(enc *json.Encoder, header *envelopeHeader) error {
	return enc.Encode(header)
}

func envelopeFromBody(event *Event, dsn *Dsn, sentAt time.Time, body json.RawMessage) (*bytes.Buffer, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)

	// Construct the trace envelope header
	var trace = map[string]string{}
	if dsc := event.sdkMetaData.dsc; dsc.HasEntries() {
		for k, v := range dsc.Entries {
			trace[k] = v
		}
	}

	// Envelope header
	err := encodeEnvelopeHeader(enc, &envelopeHeader{
		EventID: event.EventID,
		SentAt:  sentAt,
		Trace:   trace,
		Dsn:     dsn,
		Sdk: map[string]string{
			"name":    event.Sdk.Name,
			"version": event.Sdk.Version,
		},
	})
	if err != nil {
		return nil, err
	}

	switch event.Type {
	case transactionType, checkInType:
		err = encodeEnvelopeItem(enc, event.Type, body)
	case logEvent.Type:
		err = encodeEnvelopeLogs(enc, len(event.Logs), body)
	case traceMetricEvent.Type:
		err = encodeEnvelopeMetrics(enc, len(event.Metrics), body)
	default:
		err = encodeEnvelopeItem(enc, eventType, body)
	}

	if err != nil {
		return nil, err
	}

	// Attachments
	for _, attachment := range event.Attachments {
		if err := encodeAttachment(enc, &b, attachment); err != nil {
			return nil, err
		}
	}

	return &b, nil
}

// getRequestFromEnvelope creates an HTTP request from a pre-built envelope.
// sdkName and sdkVersion are used for User-Agent and authentication headers.
func getRequestFromEnvelope(ctx context.Context, dsn *Dsn, envelope *bytes.Buffer, sdkName, sdkVersion string) (*http.Request, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		dsn.GetAPIURL().String(),
		envelope,
	)
	if err != nil {
		return nil, err
	}

	request.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkName, sdkVersion))
	request.Header.Set("Content-Type", "application/x-sentry-envelope")

	auth := fmt.Sprintf("Sentry sentry_version=%s, "+
		"sentry_client=%s/%s, sentry_key=%s", apiVersion, sdkName, sdkVersion, dsn.GetPublicKey())

	// The key sentry_secret is effectively deprecated and no longer needs to be set.
	// However, since it was required in older self-hosted versions,
	// it should still be passed through to Sentry if set.
	if dsn.GetSecretKey() != "" {
		auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
	}

	request.Header.Set("X-Sentry-Auth", auth)

	return request, nil
}

// ================================
// HTTPTransport
// ================================

// A batch groups items that are processed sequentially.
type batch struct {
	items   chan batchItem
	started chan struct{} // closed to signal items started to be worked on
	done    chan struct{} // closed to signal completion of all items
}

type batchItem struct {
	ctx             context.Context
	envelope        *bytes.Buffer
	sdkName         string
	sdkVersion      string
	category        ratelimit.Category
	eventIdentifier string
	spanCount       int
	logItemCount    int
	logByteSize     int
}

// HTTPTransport is the default, non-blocking, implementation of Transport.
//
// Clients using this transport will enqueue requests in a buffer and return to
// the caller before any network communication has happened. Requests are sent
// to Sentry sequentially from a background goroutine.
type HTTPTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport http.RoundTripper
	recorder  report.ClientReportRecorder
	provider  report.ClientReportProvider

	// buffer is a channel of batches. Calling Flush terminates work on the
	// current in-flight items and starts a new batch for subsequent events.
	buffer chan batch

	startOnce sync.Once
	closeOnce sync.Once

	// Size of the transport buffer. Defaults to 30.
	BufferSize int
	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration

	mu     sync.RWMutex
	limits ratelimit.Map

	// receiving signal will terminate worker.
	done chan struct{}
}

// NewHTTPTransport returns a new pre-configured instance of HTTPTransport.
func NewHTTPTransport() *HTTPTransport {
	transport := HTTPTransport{
		BufferSize: defaultBufferSize,
		Timeout:    defaultTimeout,
		done:       make(chan struct{}),
		limits:     make(ratelimit.Map),
	}
	return &transport
}

// Configure is called by the Client itself, providing its own ClientOptions.
func (t *HTTPTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		debuglog.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

	if t.recorder == nil {
		t.recorder = report.NoopRecorder()
	}
	if t.provider == nil {
		t.provider = report.NoopProvider()
	}

	// A buffered channel with capacity 1 works like a mutex, ensuring only one
	// goroutine can access the current batch at a given time. Access is
	// synchronized by reading from and writing to the channel.
	t.buffer = make(chan batch, 1)
	t.buffer <- batch{
		items:   make(chan batchItem, t.BufferSize),
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}

	if options.HTTPTransport != nil {
		t.transport = options.HTTPTransport
	} else {
		t.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		t.client = options.HTTPClient
	} else {
		t.client = &http.Client{
			Transport: t.transport,
			Timeout:   t.Timeout,
		}
	}

	t.startOnce.Do(func() {
		go t.worker()
	})
}

// SendEvent assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEvent(event *Event) {
	t.SendEventWithContext(context.Background(), event)
}

// SendEventWithContext assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEventWithContext(ctx context.Context, event *Event) {
	if t.dsn == nil {
		return
	}

	category := event.toCategory()

	if t.disabled(category) {
		recordForEvent(t.recorder, report.ReasonRateLimitBackoff, event)
		return
	}

	body := getRequestBodyFromEvent(event)
	if body == nil {
		recordForEvent(t.recorder, report.ReasonInternalError, event)
		return
	}
	envelope, err := envelopeFromBody(event, t.dsn, time.Now(), body)
	if err != nil {
		debuglog.Printf("Failed to build envelope, skipping delivery. %s: %v", eventDebugContext(event), err)
		recordForEvent(t.recorder, report.ReasonInternalError, event)
		return
	}

	// <-t.buffer is equivalent to acquiring a lock to access the current batch.
	// A few lines below, t.buffer <- b releases the lock.
	//
	// The lock must be held during the select block below to guarantee that
	// b.items is not closed while trying to send to it. Remember that sending
	// on a closed channel panics.
	//
	// Note that the select block takes a bounded amount of CPU time because of
	// the default case that is executed if sending on b.items would block. That
	// is, the event is dropped if it cannot be sent immediately to the b.items
	// channel (used as a queue).
	b := <-t.buffer

	identifier := eventIdentifier(event)

	select {
	case b.items <- batchItem{
		ctx:             ctx,
		envelope:        envelope,
		sdkName:         event.Sdk.Name,
		sdkVersion:      event.Sdk.Version,
		category:        category,
		eventIdentifier: identifier,
		spanCount:       event.GetSpanCount(),
		logItemCount:    len(event.Logs),
		logByteSize:     event.GetLogByteSize(),
	}:
		debuglog.Printf(
			"Sending %s to %s project: %s",
			identifier,
			t.dsn.GetHost(),
			t.dsn.GetProjectID(),
		)
	default:
		debuglog.Printf("Event dropped due to transport buffer being full. %s", eventDebugContext(event))
		recordForEvent(t.recorder, report.ReasonQueueOverflow, event)
	}

	t.buffer <- b
}

// Flush waits until any buffered events are sent to the Sentry server, blocking
// for at most the given timeout. It returns false if the timeout was reached.
// In that case, some events may not have been sent.
//
// Flush should be called before terminating the program to avoid
// unintentionally dropping events.
//
// Do not call Flush indiscriminately after every call to SendEvent. Instead, to
// have the SDK send events over the network synchronously, configure it to use
// the HTTPSyncTransport in the call to Init.
func (t *HTTPTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

// FlushWithContext works like Flush, but it accepts a context.Context instead of a timeout.
func (t *HTTPTransport) FlushWithContext(ctx context.Context) bool {
	return t.flushInternal(ctx.Done())
}

func (t *HTTPTransport) flushInternal(timeout <-chan struct{}) bool {
	// Wait until processing the current batch has started or the timeout.
	//
	// We must wait until the worker has seen the current batch, because it is
	// the only way b.done will be closed. If we do not wait, there is a
	// possible execution flow in which b.done is never closed, and the only way
	// out of Flush would be waiting for the timeout, which is undesired.
	var b batch

	for {
		select {
		case b = <-t.buffer:
			select {
			case <-b.started:
				goto started
			default:
				t.buffer <- b
			}
		case <-timeout:
			goto fail
		}
	}

started:
	// Signal that there won't be any more items in this batch, so that the
	// worker inner loop can end.
	close(b.items)
	// Start a new batch for subsequent events.
	t.buffer <- batch{
		items:   make(chan batchItem, t.BufferSize),
		started: make(chan struct{}),
		done:    make(chan struct{}),
	}

	// Wait until the current batch is done or the timeout.
	select {
	case <-b.done:
		debuglog.Println("Buffer flushed successfully.")
		return true
	case <-timeout:
		goto fail
	}

fail:
	debuglog.Println("Buffer flushing was canceled or timed out. Some events may not have been sent.")
	return false
}

// Close will terminate events sending loop.
// It useful to prevent goroutines leak in case of multiple HTTPTransport instances initiated.
//
// Close should be called after Flush and before terminating the program
// otherwise some events may be lost.
func (t *HTTPTransport) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
	})
}

func (t *HTTPTransport) worker() {
	crTicker := time.NewTicker(defaultClientReportsTick)
	defer crTicker.Stop()
	for b := range t.buffer {
		// Signal that processing of the current batch has started.
		close(b.started)

		// Return the batch to the buffer so that other goroutines can use it.
		// Equivalent to releasing a lock.
		t.buffer <- b

		// Process all batch items.
	loop:
		for {
			select {
			case <-t.done:
				return
			case <-crTicker.C:
				r := t.provider.TakeReport()
				if r != nil {
					var buf bytes.Buffer
					enc := json.NewEncoder(&buf)
					if err := encodeEnvelopeHeader(enc, &envelopeHeader{
						SentAt: time.Now(),
						Dsn:    t.dsn,
						Sdk: map[string]string{
							"name":    sdkIdentifier,
							"version": SDKVersion,
						},
					}); err != nil {
						continue
					}
					if err := encodeClientReport(enc, r); err != nil {
						continue
					}
					req, err := getRequestFromEnvelope(context.Background(), t.dsn, &buf, sdkIdentifier, SDKVersion)
					if err != nil {
						debuglog.Printf("There was an issue when creating the request: %v", err)
						continue
					}
					result, err := util.DoSendRequest(t.client, req, "client report")
					if err != nil {
						debuglog.Printf("There was an issue with sending an event: %v", err)
						continue
					}
					t.mu.Lock()
					t.limits.Merge(result.Limits)
					t.mu.Unlock()
				}
			case item, open := <-b.items:
				if !open {
					break loop
				}
				if t.disabled(item.category) {
					recordForBatchItem(t.recorder, report.ReasonRateLimitBackoff, &item)
					continue
				}

				// Attach accumulated client report inside the worker to avoid background queue overflows.
				t.attachClientReport(item.envelope)
				request, err := getRequestFromEnvelope(item.ctx, t.dsn, item.envelope, item.sdkName, item.sdkVersion)
				if err != nil {
					debuglog.Printf("There was an issue when creating the request: %v", err)
					recordForBatchItem(t.recorder, report.ReasonInternalError, &item)
					continue
				}

				result, err := util.DoSendRequest(t.client, request, item.eventIdentifier)
				if err != nil {
					debuglog.Printf("There was an issue with sending an event: %v", err)
					recordForBatchItem(t.recorder, report.ReasonNetworkError, &item)
					continue
				}
				if result.IsSendError() {
					recordForBatchItem(t.recorder, report.ReasonSendError, &item)
				}

				t.mu.Lock()
				t.limits.Merge(result.Limits)
				t.mu.Unlock()
			}
		}

		// Signal that processing of the batch is done.
		close(b.done)
	}
}

// attachClientReport takes any pending client report from the provider and
// appends it to the envelope buffer.
func (t *HTTPTransport) attachClientReport(buf *bytes.Buffer) {
	r := t.provider.TakeReport()
	if r == nil {
		return
	}
	enc := json.NewEncoder(buf)
	if err := encodeClientReport(enc, r); err != nil {
		debuglog.Printf("Failed to encode client report: %v", err)
	}
}

func (t *HTTPTransport) disabled(c ratelimit.Category) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		debuglog.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
	}
	return disabled
}

// ================================
// HTTPSyncTransport
// ================================

// HTTPSyncTransport is a blocking implementation of Transport.
//
// Clients using this transport will send requests to Sentry sequentially and
// block until a response is returned.
//
// The blocking behavior is useful in a limited set of use cases. For example,
// use it when deploying code to a Function as a Service ("Serverless")
// platform, where any work happening in a background goroutine is not
// guaranteed to execute.
//
// For most cases, prefer HTTPTransport.
type HTTPSyncTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport http.RoundTripper
	recorder  report.ClientReportRecorder
	provider  report.ClientReportProvider

	mu     sync.Mutex
	limits ratelimit.Map

	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration
}

// NewHTTPSyncTransport returns a new pre-configured instance of HTTPSyncTransport.
func NewHTTPSyncTransport() *HTTPSyncTransport {
	transport := HTTPSyncTransport{
		Timeout: defaultTimeout,
		limits:  make(ratelimit.Map),
	}

	return &transport
}

// Configure is called by the Client itself, providing its own ClientOptions.
func (t *HTTPSyncTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		debuglog.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

	if t.recorder == nil {
		t.recorder = report.NoopRecorder()
	}
	if t.provider == nil {
		t.provider = report.NoopProvider()
	}

	if options.HTTPTransport != nil {
		t.transport = options.HTTPTransport
	} else {
		t.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		t.client = options.HTTPClient
	} else {
		t.client = &http.Client{
			Transport: t.transport,
			Timeout:   t.Timeout,
		}
	}
}

// SendEvent assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPSyncTransport) SendEvent(event *Event) {
	t.SendEventWithContext(context.Background(), event)
}

func (t *HTTPSyncTransport) Close() {}

// SendEventWithContext assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPSyncTransport) SendEventWithContext(ctx context.Context, event *Event) {
	if t.dsn == nil {
		return
	}

	category := event.toCategory()
	if t.disabled(category) {
		recordForEvent(t.recorder, report.ReasonRateLimitBackoff, event)
		return
	}

	body := getRequestBodyFromEvent(event)
	if body == nil {
		recordForEvent(t.recorder, report.ReasonInternalError, event)
		return
	}

	envelope, err := envelopeFromBody(event, t.dsn, time.Now(), body)
	if err != nil {
		debuglog.Printf("Failed to build envelope, skipping delivery. %s: %v", eventDebugContext(event), err)
		recordForEvent(t.recorder, report.ReasonInternalError, event)
		return
	}

	if r := t.provider.TakeReport(); r != nil {
		enc := json.NewEncoder(envelope)
		if err := encodeClientReport(enc, r); err != nil {
			debuglog.Printf("Failed to encode client report: %v", err)
		}
	}

	request, err := getRequestFromEnvelope(ctx, t.dsn, envelope, event.Sdk.Name, event.Sdk.Version)
	if err != nil {
		recordForEvent(t.recorder, report.ReasonInternalError, event)
		return
	}

	identifier := eventIdentifier(event)
	debuglog.Printf(
		"Sending %s to %s project: %s",
		identifier,
		t.dsn.GetHost(),
		t.dsn.GetProjectID(),
	)

	result, err := util.DoSendRequest(t.client, request, identifier)
	if err != nil {
		debuglog.Printf("There was an issue with sending an event: %v", err)
		recordForEvent(t.recorder, report.ReasonNetworkError, event)
		return
	}
	if result.IsSendError() {
		recordForEvent(t.recorder, report.ReasonSendError, event)
	}

	t.mu.Lock()
	t.limits.Merge(result.Limits)
	t.mu.Unlock()
}

// Flush is a no-op for HTTPSyncTransport. It always returns true immediately.
func (t *HTTPSyncTransport) Flush(_ time.Duration) bool {
	return true
}

// FlushWithContext is a no-op for HTTPSyncTransport. It always returns true immediately.
func (t *HTTPSyncTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *HTTPSyncTransport) disabled(c ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		debuglog.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
	}
	return disabled
}

// ================================
// noopTransport
// ================================

// noopTransport is an implementation of Transport interface which drops all the events.
// Only used internally when an empty DSN is provided, which effectively disables the SDK.
type noopTransport struct{}

var _ Transport = noopTransport{}

func (noopTransport) Configure(options ClientOptions) {
	if options.Spotlight {
		debuglog.Println("Sentry client initialized with an empty DSN. No events will be delivered to Sentry, but Spotlight is enabled and will still receive them.")
		return
	}
	debuglog.Println("Sentry client initialized with an empty DSN. Using noopTransport. No events will be delivered.")
}

func (noopTransport) SendEvent(*Event) {
	debuglog.Println("Event dropped due to noopTransport usage.")
}

func (noopTransport) Flush(time.Duration) bool {
	return true
}

func (noopTransport) FlushWithContext(context.Context) bool {
	return true
}

func (noopTransport) Close() {}

// SpotlightTransport decorates Transport to also send events to Spotlight.
type SpotlightTransport struct {
	underlying   Transport
	client       *http.Client
	spotlightURL string

	// placeholderDsn exists only so envelopeFromBody has a Dsn to put in the
	// envelope header - requests go straight to spotlightURL, not a DSN
	// endpoint, so its value doesn't matter. Parsed once here, not per-send.
	placeholderDsn *Dsn

	// ctx is cancelled on Close to signal goroutines to stop.
	// inFlight tracks in-flight sends so Close/Flush can wait for them.
	// It's a plain counter rather than a sync.WaitGroup because SendEvent
	// can run concurrently with Flush/Close from another goroutine, and
	// WaitGroup panics if Add races with Wait on a zeroed counter.
	ctx      context.Context
	cancel   context.CancelFunc
	inFlight atomic.Int64

	backoff spotlightBackoff
}

const (
	// defaultSpotlightURL is the default address of the local Spotlight sidecar.
	defaultSpotlightURL = "http://localhost:8969/stream"

	spotlightInitialRetryDelay = time.Second
	spotlightMaxRetryDelay     = 60 * time.Second

	// spotlightWaitPollInterval is how often SpotlightTransport's Flush/Close
	// poll for in-flight Spotlight sends to complete.
	spotlightWaitPollInterval = 5 * time.Millisecond
)

// spotlightBackoff tracks connectivity backoff and log-deduplication state
// shared by anything that sends to the Spotlight sidecar. It is safe for
// concurrent use, since sends happen one goroutine per event/envelope.
type spotlightBackoff struct {
	mu          sync.Mutex
	retryDelay  time.Duration
	nextAttempt time.Time
	errorLogged bool
}

func newSpotlightBackoff() spotlightBackoff {
	return spotlightBackoff{retryDelay: spotlightInitialRetryDelay}
}

// ready reports whether enough time has passed since the last failed send to
// attempt another one, implementing a simple exponential backoff so an
// unreachable Spotlight server does not get hammered with a request per
// captured event/envelope.
func (b *spotlightBackoff) ready() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nextAttempt.IsZero() || !time.Now().Before(b.nextAttempt)
}

// recordFailure logs a connectivity error at most once per outage (further
// failures are suppressed until a send succeeds again) and schedules the next
// allowed attempt using exponential backoff, capped at spotlightMaxRetryDelay.
func (b *spotlightBackoff) recordFailure(spotlightURL string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.errorLogged {
		debuglog.Printf(
			"Failed to send to Spotlight at %s: %v. "+
				"Make sure Spotlight is running (npm install -g @spotlightjs/spotlight && spotlight). "+
				"Further connectivity errors will be suppressed until Spotlight is reachable again.",
			spotlightURL, err,
		)
		b.errorLogged = true
	}

	// Concurrent sends can race past ready() before one of them extends the
	// backoff window; only let the one that actually starts/restarts the
	// outage push it out, so stragglers don't compound the delay.
	now := time.Now()
	if !b.nextAttempt.IsZero() && now.Before(b.nextAttempt) {
		return
	}

	b.nextAttempt = now.Add(b.retryDelay)
	if b.retryDelay < spotlightMaxRetryDelay {
		b.retryDelay = min(b.retryDelay*2, spotlightMaxRetryDelay)
	}
}

// recordSuccess resets the backoff/log-dedup state after a successful send.
func (b *spotlightBackoff) recordSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.retryDelay = spotlightInitialRetryDelay
	b.nextAttempt = time.Time{}
	b.errorLogged = false
}

func NewSpotlightTransport(underlying Transport) *SpotlightTransport {
	ctx, cancel := context.WithCancel(context.Background())

	placeholderDsn, err := NewDsn("https://placeholder@localhost/1")
	if err != nil {
		// Unreachable in practice: the DSN above is a fixed, valid literal.
		debuglog.Printf("Failed to create Spotlight placeholder DSN: %v", err)
	}

	return &SpotlightTransport{
		underlying:     underlying,
		placeholderDsn: placeholderDsn,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		spotlightURL: defaultSpotlightURL,
		ctx:          ctx,
		cancel:       cancel,
		backoff:      newSpotlightBackoff(),
	}
}

// readyToSend reports whether enough time has passed since the last failed
// Spotlight send to attempt another one, implementing a simple exponential
// backoff so an unreachable Spotlight server does not get hammered with a
// request per captured event.
func (st *SpotlightTransport) readyToSend() bool {
	return st.backoff.ready()
}

func (st *SpotlightTransport) recordFailure(err error) {
	st.backoff.recordFailure(st.spotlightURL, err)
}

func (st *SpotlightTransport) recordSuccess() {
	st.backoff.recordSuccess()
}

func (st *SpotlightTransport) Configure(options ClientOptions) {
	st.underlying.Configure(options)

	if options.SpotlightURL != "" {
		st.spotlightURL = options.SpotlightURL
	}

	st.buildHTTPClient(options)
}

func (st *SpotlightTransport) buildHTTPClient(opts ClientOptions) {
	st.client = buildSpotlightHTTPClient(opts)
}

// buildSpotlightHTTPClient builds an *http.Client for talking to the
// Spotlight sidecar, respecting the user's proxy/TLS/custom HTTPClient or
// HTTPTransport settings while enforcing Spotlight's own fixed timeout.
// Spotlight is dev-only; a long or zero timeout inherited from the user's
// client would defeat the point of never blocking normal operation.
func buildSpotlightHTTPClient(opts ClientOptions) *http.Client {
	if opts.HTTPClient != nil {
		return &http.Client{
			Transport:     opts.HTTPClient.Transport,
			CheckRedirect: opts.HTTPClient.CheckRedirect,
			Jar:           opts.HTTPClient.Jar,
			Timeout:       5 * time.Second,
		}
	}

	var transport http.RoundTripper
	if opts.HTTPTransport != nil {
		transport = opts.HTTPTransport
	} else {
		transport = &http.Transport{
			Proxy:           getProxyConfig(opts),
			TLSClientConfig: getTLSConfig(opts),
		}
	}

	return &http.Client{
		Transport: transport,
		Timeout:   5 * time.Second,
	}
}

// SendEvent serializes event and builds the Spotlight envelope synchronously,
// before returning, so nothing touches event's mutable fields (maps like
// Contexts/Extra/Tags) after the caller regains control of it. Only the
// network request itself runs in the background goroutine.
func (st *SpotlightTransport) SendEvent(event *Event) {
	st.underlying.SendEvent(event)

	if st.isShuttingDown() {
		debuglog.Printf("Skipping Spotlight send: transport shutting down")
		return
	}
	if !st.readyToSend() {
		// Backing off after a recent connectivity failure; drop this event
		// rather than hammering an unreachable Spotlight server.
		return
	}
	if st.placeholderDsn == nil {
		debuglog.Println("Skipping Spotlight send: no placeholder DSN available")
		return
	}

	eventBody := getRequestBodyFromEvent(event)
	if eventBody == nil {
		debuglog.Println("Failed to serialize event for Spotlight")
		return
	}

	envelope, err := envelopeFromBody(event, st.placeholderDsn, time.Now(), eventBody)
	if err != nil {
		debuglog.Printf("Failed to create Spotlight envelope: %v", err)
		return
	}

	st.inFlight.Add(1)
	go func() {
		defer st.inFlight.Add(-1)
		st.postToSpotlight(envelope)
	}()
}

func (st *SpotlightTransport) isShuttingDown() bool {
	return st.ctx.Err() != nil
}

func (st *SpotlightTransport) postToSpotlight(envelope *bytes.Buffer) {
	timeoutCtx, cancel := context.WithTimeout(st.ctx, st.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "POST", st.spotlightURL, envelope)
	if err != nil {
		debuglog.Printf("Failed to create Spotlight request: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkIdentifier, SDKVersion))

	resp, err := st.client.Do(req)
	if err != nil {
		st.recordFailure(err)
		return
	}
	defer func() {
		// Drain body up to a limit and close it, allowing the
		// transport to reuse TCP connections.
		_, _ = io.CopyN(io.Discard, resp.Body, util.MaxDrainResponseBytes)
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Printf("Failed to close Spotlight response body: %v", closeErr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		st.recordFailure(fmt.Errorf("spotlight server returned status %d", resp.StatusCode))
	} else {
		st.recordSuccess()
	}
}

// Flush waits for the underlying transport to send pending events to Sentry,
// and for any in-flight Spotlight sends to complete, up to timeout.
func (st *SpotlightTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return st.FlushWithContext(ctx)
}

// FlushWithContext waits for the underlying transport to send pending events
// to Sentry, and for any in-flight Spotlight sends to complete, up until ctx
// is done.
func (st *SpotlightTransport) FlushWithContext(ctx context.Context) bool {
	underlyingOK := st.underlying.FlushWithContext(ctx)

	spotlightOK := util.WaitForZero(ctx, &st.inFlight, spotlightWaitPollInterval)
	if !spotlightOK {
		debuglog.Printf("Timed out waiting for in-flight Spotlight sends to complete")
	}

	return underlyingOK && spotlightOK
}

func (st *SpotlightTransport) Close() {
	st.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if util.WaitForZero(ctx, &st.inFlight, spotlightWaitPollInterval) {
		debuglog.Printf("All Spotlight sends completed")
	} else {
		debuglog.Printf("Spotlight sends timed out during shutdown")
	}

	st.underlying.Close()
}

// cloneEnvelopeForSpotlight copies an envelope so the Spotlight send doesn't
// share mutable state with the original, which keeps going through the real
// transport concurrently.
func cloneEnvelopeForSpotlight(envelope *protocol.Envelope) *protocol.Envelope {
	var header *protocol.EnvelopeHeader
	if envelope.Header != nil {
		h := *envelope.Header
		header = &h
	}

	cloned := protocol.NewEnvelope(header)
	for _, item := range envelope.Items {
		if item == nil {
			continue
		}
		cloned.AddItem(&protocol.EnvelopeItem{
			Header:  cloneEnvelopeItemHeader(item.Header),
			Payload: append([]byte(nil), item.Payload...),
		})
	}
	return cloned
}

// cloneEnvelopeItemHeader deep-copies an EnvelopeItemHeader, including its
// Length and ItemCount pointer fields.
func cloneEnvelopeItemHeader(header *protocol.EnvelopeItemHeader) *protocol.EnvelopeItemHeader {
	if header == nil {
		return nil
	}
	newHeader := *header
	if header.Length != nil {
		length := *header.Length
		newHeader.Length = &length
	}
	if header.ItemCount != nil {
		itemCount := *header.ItemCount
		newHeader.ItemCount = &itemCount
	}
	return &newHeader
}

// spotlightEnvelopeSender implements telemetry.SpotlightSender, forwarding a
// copy of every envelope the internal/telemetry scheduler sends to a local
// Spotlight sidecar. This is what makes Spotlight work on the default
// transport path; SpotlightTransport below handles the legacy path instead
// (custom Transport / HTTPTransport / HTTPSyncTransport).
type spotlightEnvelopeSender struct {
	client       *http.Client
	spotlightURL string

	// ctx is cancelled on Close to signal in-flight sends to stop.
	// inFlight tracks in-flight sends so Close/FlushWithContext can wait on
	// them. It's a plain counter, not a sync.WaitGroup: Send can be called
	// from the scheduler's background run() goroutine while a concurrent
	// client.Flush() call from a different goroutine is also happening
	// (e.g. a new event captured while Flush is in progress), and WaitGroup
	// panics if Add races with Wait on a zeroed counter.
	ctx      context.Context
	cancel   context.CancelFunc
	inFlight atomic.Int64

	backoff spotlightBackoff
}

func newSpotlightEnvelopeSender(opts ClientOptions) *spotlightEnvelopeSender {
	ctx, cancel := context.WithCancel(context.Background())

	url := opts.SpotlightURL
	if url == "" {
		url = defaultSpotlightURL
	}

	return &spotlightEnvelopeSender{
		client:       buildSpotlightHTTPClient(opts),
		spotlightURL: url,
		ctx:          ctx,
		cancel:       cancel,
		backoff:      newSpotlightBackoff(),
	}
}

// Send implements telemetry.SpotlightSender. It clones the envelope before
// returning, since the scheduler hands the same pointer to the real
// transport, which mutates it later (e.g. attaching a client report) on its
// own background worker. Only the network request itself runs async.
func (s *spotlightEnvelopeSender) Send(envelope *protocol.Envelope) {
	if s.ctx.Err() != nil {
		debuglog.Printf("Skipping Spotlight send: shutting down")
		return
	}
	if !s.backoff.ready() {
		// Backing off after a recent connectivity failure; drop this
		// envelope rather than hammering an unreachable Spotlight server.
		return
	}

	cloned := cloneEnvelopeForSpotlight(envelope)

	s.inFlight.Add(1)
	go func() {
		defer s.inFlight.Add(-1)
		s.send(cloned)
	}()
}

func (s *spotlightEnvelopeSender) send(envelope *protocol.Envelope) {
	body, err := envelope.Serialize()
	if err != nil {
		debuglog.Printf("Failed to serialize envelope for Spotlight: %v", err)
		return
	}

	timeoutCtx, cancel := context.WithTimeout(s.ctx, s.client.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, "POST", s.spotlightURL, bytes.NewReader(body))
	if err != nil {
		debuglog.Printf("Failed to create Spotlight request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkIdentifier, SDKVersion))

	resp, err := s.client.Do(req)
	if err != nil {
		s.backoff.recordFailure(s.spotlightURL, err)
		return
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, util.MaxDrainResponseBytes)
		if closeErr := resp.Body.Close(); closeErr != nil {
			debuglog.Printf("Failed to close Spotlight response body: %v", closeErr)
		}
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.backoff.recordFailure(s.spotlightURL, fmt.Errorf("spotlight server returned status %d", resp.StatusCode))
	} else {
		s.backoff.recordSuccess()
	}
}

// FlushWithContext implements telemetry.SpotlightSender.
func (s *spotlightEnvelopeSender) FlushWithContext(ctx context.Context) bool {
	if util.WaitForZero(ctx, &s.inFlight, spotlightWaitPollInterval) {
		return true
	}
	debuglog.Printf("Timed out waiting for in-flight Spotlight sends to complete")
	return false
}

// Close implements telemetry.SpotlightSender.
func (s *spotlightEnvelopeSender) Close() {
	s.cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if util.WaitForZero(ctx, &s.inFlight, spotlightWaitPollInterval) {
		debuglog.Printf("All Spotlight sends completed")
	} else {
		debuglog.Printf("Spotlight sends timed out during shutdown")
	}
}

// ================================
// Internal Transport Adapters
// ================================

// newInternalAsyncTransport creates a new AsyncTransport from internal/http
// wrapped to satisfy the Transport interface.
//
// This is not yet exposed in the public API and is for internal experimentation.
func newInternalAsyncTransport() Transport {
	return &internalAsyncTransportAdapter{}
}

// internalAsyncTransportAdapter wraps the internal AsyncTransport to implement
// the root-level Transport interface.
type internalAsyncTransportAdapter struct {
	transport protocol.TelemetryTransport
	dsn       *protocol.Dsn
	recorder  report.ClientReportRecorder
	provider  report.ClientReportProvider
}

func (a *internalAsyncTransportAdapter) Configure(options ClientOptions) {
	transportOptions := httpinternal.TransportOptions{
		Dsn:           options.Dsn,
		HTTPClient:    options.HTTPClient,
		HTTPTransport: options.HTTPTransport,
		HTTPProxy:     options.HTTPProxy,
		HTTPSProxy:    options.HTTPSProxy,
		CaCerts:       options.CaCerts,
		Recorder:      a.recorder,
		Provider:      a.provider,
		SdkInfo: func() *protocol.SdkInfo {
			return &protocol.SdkInfo{
				Name:    sdkIdentifier,
				Version: SDKVersion,
			}
		},
	}

	a.transport = httpinternal.NewAsyncTransport(transportOptions)

	if options.Dsn != "" {
		dsn, err := protocol.NewDsn(options.Dsn)
		if err != nil {
			debuglog.Printf("Failed to parse DSN in adapter: %v\n", err)
		} else {
			a.dsn = dsn
		}
	}
}

func (a *internalAsyncTransportAdapter) SendEvent(event *Event) {
	header := &protocol.EnvelopeHeader{EventID: string(event.EventID), SentAt: time.Now(), Dsn: a.dsn, Sdk: &protocol.SdkInfo{Name: event.Sdk.Name, Version: event.Sdk.Version}}
	if header.EventID == "" {
		header.EventID = protocol.GenerateEventID()
	}
	envelope, err := event.ToEnvelope(header)
	if err != nil {
		debuglog.Printf("Failed to convert event to envelope, skipping delivery. %s: %v", eventDebugContext(event), err)
		if a.recorder != nil {
			recordForEvent(a.recorder, report.ReasonInternalError, event)
		}
		return
	}

	if err := a.transport.SendEnvelope(envelope); err != nil {
		debuglog.Printf("Error sending envelope: %v", err)
	}
}

func (a *internalAsyncTransportAdapter) Flush(timeout time.Duration) bool {
	return a.transport.Flush(timeout)
}

func (a *internalAsyncTransportAdapter) FlushWithContext(ctx context.Context) bool {
	return a.transport.FlushWithContext(ctx)
}

func (a *internalAsyncTransportAdapter) Close() {
	a.transport.Close()
}
