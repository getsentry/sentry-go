package sentry

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

const defaultTimeout = time.Second * 30

// maxDrainResponseBytes is the maximum number of bytes that transport
// implementations will read from response bodies when draining them.
//
// Sentry's ingestion API responses are typically short and the SDK doesn't need
// the contents of the response body. However, the net/http HTTP client requires
// response bodies to be fully drained (and closed) for TCP keep-alive to work.
//
// maxDrainResponseBytes strikes a balance between reading too much data (if the
// server is misbehaving) and reusing TCP connections.
const maxDrainResponseBytes = 16 << 10

// Transport is used by the Client to deliver events to remote server.
type Transport interface {
	Flush(timeout time.Duration) bool
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

func getRequestBodyFromEvent(event *Event) []byte {
	body, err := json.Marshal(event)
	if err == nil {
		return body
	}

	msg := fmt.Sprintf("Could not encode original event as JSON. "+
		"Succeeded by removing Breadcrumbs, Contexts and Extra. "+
		"Please verify the data you attach to the scope. "+
		"Error: %s", err)
	// Try to serialize the event, with all the contextual data that allows for interface{} stripped.
	event.Breadcrumbs = nil
	event.Contexts = nil
	event.Extra = map[string]interface{}{
		"info": msg,
	}
	body, err = json.Marshal(event)
	if err == nil {
		DebugLogger.Println(msg)
		return body
	}

	// This should _only_ happen when Event.Exception[0].Stacktrace.Frames[0].Vars is unserializable
	// Which won't ever happen, as we don't use it now (although it's the part of public interface accepted by Sentry)
	// Juuust in case something, somehow goes utterly wrong.
	DebugLogger.Println("Event couldn't be marshaled, even with stripped contextual data. Skipping delivery. " +
		"Please notify the SDK owners with possibly broken payload.")
	return nil
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

func encodeEnvelopeLogs(enc *json.Encoder, itemsLength int, body json.RawMessage) error {
	err := enc.Encode(
		struct {
			Type        string `json:"type"`
			ItemCount   int    `json:"item_count"`
			ContentType string `json:"content_type"`
		}{
			Type:        logEvent.Type,
			ItemCount:   itemsLength,
			ContentType: logEvent.ContentType,
		})
	if err == nil {
		err = enc.Encode(body)
	}
	return err
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
	err := enc.Encode(struct {
		EventID EventID           `json:"event_id"`
		SentAt  time.Time         `json:"sent_at"`
		Dsn     string            `json:"dsn"`
		Sdk     map[string]string `json:"sdk"`
		Trace   map[string]string `json:"trace,omitempty"`
	}{
		EventID: event.EventID,
		SentAt:  sentAt,
		Trace:   trace,
		Dsn:     dsn.String(),
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
	case logType:
		err = encodeEnvelopeLogs(enc, len(event.Logs), body)
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

func getRequestFromEvent(ctx context.Context, event *Event, dsn *Dsn) (r *http.Request, err error) {
	defer func() {
		if r != nil {
			r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", event.Sdk.Name, event.Sdk.Version))
			r.Header.Set("Content-Type", "application/x-sentry-envelope")

			auth := fmt.Sprintf("Sentry sentry_version=%s, "+
				"sentry_client=%s/%s, sentry_key=%s", apiVersion, event.Sdk.Name, event.Sdk.Version, dsn.publicKey)

			// The key sentry_secret is effectively deprecated and no longer needs to be set.
			// However, since it was required in older self-hosted versions,
			// it should still be passed through to Sentry if set.
			if dsn.secretKey != "" {
				auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.secretKey)
			}

			r.Header.Set("X-Sentry-Auth", auth)
		}
	}()

	body := getRequestBodyFromEvent(event)
	if body == nil {
		return nil, errors.New("event could not be marshaled")
	}

	envelope, err := envelopeFromBody(event, dsn, time.Now(), body)
	if err != nil {
		return nil, err
	}

	if ctx == nil {
		ctx = context.Background()
	}

	return http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		dsn.GetAPIURL().String(),
		envelope,
	)
}

func categoryFor(eventType string) ratelimit.Category {
	switch eventType {
	case "":
		return ratelimit.CategoryError
	case transactionType:
		return ratelimit.CategoryTransaction
	default:
		return ratelimit.Category(eventType)
	}
}

// ================================
// HTTPTransport
// ================================

// HTTPTransport is the default, non-blocking, implementation of Transport.
//
// Clients using this transport will enqueue requests in buffers and return to
// the caller before any network communication has happened. Requests are sent
// to Sentry sequentially from a background goroutine.
type HTTPTransport struct {
	dsn          *Dsn
	client       *http.Client
	transport    http.RoundTripper
	buffers      map[BufferType]Buffer
	bufferSignal map[BufferType]chan struct{}

	start  sync.Once
	mu     sync.RWMutex
	wg     sync.WaitGroup
	limits ratelimit.Map

	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration
	// shutdownFunc terminates the async worker.
	shutdownFunc context.CancelFunc
}

// NewHTTPTransport returns a new pre-configured instance of HTTPTransport.
func NewHTTPTransport() *HTTPTransport {
	transport := HTTPTransport{
		Timeout:      defaultTimeout,
		buffers:      make(map[BufferType]Buffer),
		bufferSignal: make(map[BufferType]chan struct{}),
	}
	return &transport
}

func (t *HTTPTransport) SetBuffer(bufferType BufferType, size int, batchSize int, priority Priority, timeout time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.buffers[bufferType] = NewBuffer(bufferType, size, batchSize, priority, timeout)
	t.bufferSignal[bufferType] = make(chan struct{}, 1)
}

// Configure is called by the Client itself, providing its own ClientOptions.
func (t *HTTPTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		DebugLogger.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

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

	t.SetBuffer(ErrorBuffer, 30, 1, 1, 5*time.Second)
	if options.EnableLogs {
		t.SetBuffer(LogBuffer, 1000, 100, 3, 5*time.Second)
	}
	if options.EnableTracing {
		t.SetBuffer(TransactionBuffer, 1000, 1, 2, 5*time.Second)
	}
	t.Start()
}

func (t *HTTPTransport) Start() {
	t.start.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		t.shutdownFunc = cancel
		t.mu.RLock()
		for bufType, buffer := range t.buffers {
			t.wg.Add(1)
			go t.worker(ctx, bufType, buffer)
		}
		t.mu.RUnlock()
	})
}

func (t *HTTPTransport) createAndSendRequest(ctx context.Context, events []*Event) {
	if len(events) == 0 {
		return
	}

	for _, event := range events {
		if t.dsn == nil {
			return
		}

		category := categoryFor(event.Type)
		if t.disabled(category) {
			return
		}

		request, err := getRequestFromEvent(ctx, event, t.dsn)
		if err != nil {
			return
		}

		var eventIdentifier string
		switch event.Type {
		case transactionType:
			eventIdentifier = "transaction"
		case logType:
			eventIdentifier = fmt.Sprintf("%v log events", len(event.Logs))
		default:
			eventIdentifier = fmt.Sprintf("%s event", event.Level)
		}
		DebugLogger.Printf(
			"Sending %s [%s] to %s project: %s",
			eventIdentifier,
			event.EventID,
			t.dsn.host,
			t.dsn.projectID,
		)

		response, err := t.client.Do(request)
		if err != nil {
			DebugLogger.Printf("There was an issue with sending an event: %v", err)
			return
		}
		if response.StatusCode >= 400 && response.StatusCode <= 599 {
			b, err := io.ReadAll(response.Body)
			if err != nil {
				DebugLogger.Printf("Error while reading response code: %v", err)
			}
			DebugLogger.Printf("Sending %s failed with the following error: %s", eventIdentifier, string(b))
		}

		t.mu.Lock()
		if t.limits == nil {
			t.limits = make(ratelimit.Map)
		}

		t.limits.Merge(ratelimit.FromResponse(response))
		t.mu.Unlock()

		// Drain body up to a limit and close it, allowing the
		// transport to reuse TCP connections.
		_, _ = io.CopyN(io.Discard, response.Body, maxDrainResponseBytes)
		response.Body.Close()
	}
}

// SendEvent assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEvent(event *Event) {
	t.SendEventWithContext(context.Background(), event)
}

// SendEventWithContext assembles a new packet out of Event and sends it to the remote server.
func (t *HTTPTransport) SendEventWithContext(_ context.Context, event *Event) {
	bufType := eventToBuffer(event.Type)
	buffer, ok := t.buffers[bufType]
	if !ok || bufType == InvalidBuffer {
		DebugLogger.Printf("Event with type: %v dropped due to buffer not set correctly", event.Type)
		return
	}

	buffer.AddItem(event)
	if buffer.HasBatchSize() {
		t.bufferSignal[bufType] <- struct{}{}
	}
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
	done := make(chan struct{})
	go func() {
		t.mu.RLock()
		for bufType := range t.buffers {
			select {
			case t.bufferSignal[bufType] <- struct{}{}:
			default: // prevent blocking if signal already sent
			}
		}
		t.mu.RUnlock()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Close will terminate events sending loop.
// It useful to prevent goroutines leak in case of multiple HTTPTransport instances initiated.
//
// Close should be called after Flush and before terminating the program
// otherwise some events may be lost.
func (t *HTTPTransport) Close() {
	if t.shutdownFunc == nil {
		return
	}
	t.shutdownFunc()
}

func (t *HTTPTransport) worker(ctx context.Context, bufType BufferType, buffer Buffer) {
	defer t.wg.Done()
	ticker := time.NewTicker(buffer.Timeout())
	defer ticker.Stop()

	newEventSig := t.bufferSignal[bufType]
	for {
		select {
		case <-ctx.Done():
			events := buffer.FlushItems()
			if len(events) > 0 {
				t.createAndSendRequest(context.Background(), events)
			}
			return
		case <-ticker.C:
			events := buffer.FlushItems()
			if len(events) > 0 {
				t.createAndSendRequest(context.Background(), events)
			}
		case <-newEventSig:
			events := buffer.FlushItems()
			if len(events) > 0 {
				t.createAndSendRequest(ctx, events)
			}
		}
	}
}

func (t *HTTPTransport) disabled(c ratelimit.Category) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		DebugLogger.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
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

// Configure is called by the Client itself, providing it, its own ClientOptions.
func (t *HTTPSyncTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		DebugLogger.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

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

	if t.disabled(categoryFor(event.Type)) {
		return
	}

	request, err := getRequestFromEvent(ctx, event, t.dsn)
	if err != nil {
		return
	}

	var eventIdentifier string
	switch event.Type {
	case transactionType:
		eventIdentifier = "transaction"
	case logType:
		eventIdentifier = fmt.Sprintf("%v log events", len(event.Logs))
	default:
		eventIdentifier = fmt.Sprintf("%s event", event.Level)
	}
	DebugLogger.Printf(
		"Sending %s [%s] to %s project: %s",
		eventIdentifier,
		event.EventID,
		t.dsn.host,
		t.dsn.projectID,
	)

	response, err := t.client.Do(request)
	if err != nil {
		DebugLogger.Printf("There was an issue with sending an event: %v", err)
		return
	}
	if response.StatusCode >= 400 && response.StatusCode <= 599 {
		b, err := io.ReadAll(response.Body)
		if err != nil {
			DebugLogger.Printf("Error while reading response code: %v", err)
		}
		DebugLogger.Printf("Sending %s failed with the following error: %s", eventIdentifier, string(b))
	}

	t.mu.Lock()
	if t.limits == nil {
		t.limits = make(ratelimit.Map)
	}

	t.limits.Merge(ratelimit.FromResponse(response))
	t.mu.Unlock()

	// Drain body up to a limit and close it, allowing the
	// transport to reuse TCP connections.
	_, _ = io.CopyN(io.Discard, response.Body, maxDrainResponseBytes)
	response.Body.Close()
}

// Flush is a no-op for HTTPSyncTransport. It always returns true immediately.
func (t *HTTPSyncTransport) Flush(_ time.Duration) bool {
	return true
}

func (t *HTTPSyncTransport) disabled(c ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		DebugLogger.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
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

func (noopTransport) Configure(ClientOptions) {
	DebugLogger.Println("Sentry client initialized with an empty DSN. Using noopTransport. No events will be delivered.")
}

func (noopTransport) SendEvent(*Event) {
	DebugLogger.Println("Event dropped due to noopTransport usage.")
}

func (noopTransport) Flush(time.Duration) bool {
	return true
}

func (noopTransport) Close() {}
