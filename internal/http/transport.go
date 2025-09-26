package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
)

const (
	defaultTimeout = time.Second * 30

	apiVersion = 7

	defaultWorkerCount    = 1
	defaultQueueSize      = 1000
	defaultRequestTimeout = 30 * time.Second
	defaultMaxRetries     = 3
	defaultRetryBackoff   = time.Second
)

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

var (
	// ErrTransportQueueFull is returned when the transport queue is full,
	// providing backpressure signal to the caller.
	ErrTransportQueueFull = errors.New("transport queue full")

	// ErrTransportClosed is returned when trying to send on a closed transport.
	ErrTransportClosed = errors.New("transport is closed")
)

// TransportOptions contains the configuration needed by the internal HTTP transports.
type TransportOptions struct {
	Dsn           string
	HTTPClient    *http.Client
	HTTPTransport http.RoundTripper
	HTTPProxy     string
	HTTPSProxy    string
	CaCerts       *x509.CertPool
	DebugLogger   *log.Logger
}

func getProxyConfig(options TransportOptions) func(*http.Request) (*url.URL, error) {
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

func getTLSConfig(options TransportOptions) *tls.Config {
	if options.CaCerts != nil {
		// #nosec G402 -- We should be using `MinVersion: tls.VersionTLS12`,
		// 				 but we don't want to break peoples code without the major bump.
		return &tls.Config{
			RootCAs: options.CaCerts,
		}
	}

	return nil
}

func getSentryRequestFromEnvelope(ctx context.Context, dsn *protocol.Dsn, envelope *protocol.Envelope) (r *http.Request, err error) {
	defer func() {
		if r != nil {
			// Extract SDK info from envelope header
			sdkName := "sentry.go"
			sdkVersion := "unknown"

			// Try to extract from envelope header if available
			if envelope.Header.Sdk != nil {
				if sdkMap, ok := envelope.Header.Sdk.(map[string]interface{}); ok {
					if name, ok := sdkMap["name"].(string); ok {
						sdkName = name
					}
					if version, ok := sdkMap["version"].(string); ok {
						sdkVersion = version
					}
				}
			}

			r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkName, sdkVersion))
			r.Header.Set("Content-Type", "application/x-sentry-envelope")

			auth := fmt.Sprintf("Sentry sentry_version=%d, "+
				"sentry_client=%s/%s, sentry_key=%s", apiVersion, sdkName, sdkVersion, dsn.GetPublicKey())

			// The key sentry_secret is effectively deprecated and no longer needs to be set.
			// However, since it was required in older self-hosted versions,
			// it should still be passed through to Sentry if set.
			if dsn.GetSecretKey() != "" {
				auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
			}

			r.Header.Set("X-Sentry-Auth", auth)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}

	// Serialize envelope to get request body
	var buf bytes.Buffer
	_, err = envelope.WriteTo(&buf)
	if err != nil {
		return nil, err
	}

	return http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		dsn.GetAPIURL().String(),
		&buf,
	)
}

// categoryFromEnvelope determines the rate limiting category from an envelope.
// Maps envelope item types to official Sentry rate limiting categories as per:
// https://develop.sentry.dev/sdk/expected-features/rate-limiting/#definitions
func categoryFromEnvelope(envelope *protocol.Envelope) ratelimit.Category {
	if envelope == nil || len(envelope.Items) == 0 {
		return ratelimit.CategoryAll
	}

	// Find the first non-attachment item to determine the primary category
	for _, item := range envelope.Items {
		if item == nil || item.Header == nil {
			continue
		}

		switch item.Header.Type {
		case protocol.EnvelopeItemTypeEvent:
			return ratelimit.CategoryError
		case protocol.EnvelopeItemTypeTransaction:
			return ratelimit.CategoryTransaction
		case protocol.EnvelopeItemTypeAttachment:
			// Skip attachments and look for the main content type
			continue
		default:
			// All other types (sessions, profiles, replays, check-ins, logs, metrics, etc.)
			// fall back to CategoryAll since we only support error and transaction specifically
			return ratelimit.CategoryAll
		}
	}

	// If we only found attachments or no valid items
	return ratelimit.CategoryAll
}

// ================================
// SyncTransport
// ================================

// SyncTransport is a blocking implementation of Transport.
//
// Clients using this transport will send requests to Sentry sequentially and
// block until a response is returned.
//
// The blocking behavior is useful in a limited set of use cases. For example,
// use it when deploying code to a Function as a Service ("Serverless")
// platform, where any work happening in a background goroutine is not
// guaranteed to execute.
//
// For most cases, prefer AsyncTransport.
type SyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	logger    *log.Logger

	mu     sync.Mutex
	limits ratelimit.Map

	// HTTP Client request timeout. Defaults to 30 seconds.
	Timeout time.Duration
}

// NewSyncTransport returns a new instance of SyncTransport configured with the given options.
func NewSyncTransport(options TransportOptions) *SyncTransport {
	transport := &SyncTransport{
		Timeout: defaultTimeout,
		limits:  make(ratelimit.Map),
		logger:  options.DebugLogger,
	}

	dsn, err := protocol.NewDsn(options.Dsn)
	if err != nil {
		if transport.logger != nil {
			transport.logger.Printf("%v\n", err)
		}
		return transport
	}
	transport.dsn = dsn

	if options.HTTPTransport != nil {
		transport.transport = options.HTTPTransport
	} else {
		transport.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		transport.client = options.HTTPClient
	} else {
		transport.client = &http.Client{
			Transport: transport.transport,
			Timeout:   transport.Timeout,
		}
	}

	return transport
}

// SendEnvelope assembles a new packet out of an Envelope and sends it to the remote server.
func (t *SyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	return t.SendEnvelopeWithContext(context.Background(), envelope)
}

func (t *SyncTransport) Close() {}

// IsRateLimited checks if a specific category is currently rate limited.
func (t *SyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.disabled(category)
}

// SendEnvelopeWithContext assembles a new packet out of an Envelope and sends it to the remote server.
func (t *SyncTransport) SendEnvelopeWithContext(ctx context.Context, envelope *protocol.Envelope) error {
	if t.dsn == nil {
		return nil
	}

	// Check rate limiting
	category := categoryFromEnvelope(envelope)
	if t.disabled(category) {
		return nil
	}

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		if t.logger != nil {
			t.logger.Printf("There was an issue creating the request: %v", err)
		}
		return err
	}
	response, err := t.client.Do(request)
	if err != nil {
		if t.logger != nil {
			t.logger.Printf("There was an issue with sending an event: %v", err)
		}
		return err
	}
	if response.StatusCode >= 400 && response.StatusCode <= 599 {
		b, err := io.ReadAll(response.Body)
		if err != nil {
			if t.logger != nil {
				t.logger.Printf("Error while reading response code: %v", err)
			}
		}
		if t.logger != nil {
			t.logger.Printf("Sending %s failed with the following error: %s", envelope.Header.EventID, string(b))
		}
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
	return response.Body.Close()
}

// Flush is a no-op for SyncTransport. It always returns true immediately.
func (t *SyncTransport) Flush(_ time.Duration) bool {
	return true
}

// FlushWithContext is a no-op for SyncTransport. It always returns true immediately.
func (t *SyncTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *SyncTransport) disabled(c ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		if t.logger != nil {
			t.logger.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
		}
	}
	return disabled
}

// Worker represents a single HTTP worker that processes envelopes.
type Worker struct {
	id        int
	transport *AsyncTransport
	done      chan struct{}
	wg        *sync.WaitGroup
}

// AsyncTransport uses a bounded worker pool for controlled concurrency and provides
// backpressure when the queue is full.
type AsyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	logger    *log.Logger

	sendQueue   chan *protocol.Envelope
	workers     []*Worker
	workerCount int

	mu     sync.RWMutex
	limits ratelimit.Map

	done   chan struct{}
	wg     sync.WaitGroup
	closed bool

	sentCount    int64
	droppedCount int64
	errorCount   int64

	// QueueSize is the capacity of the send queue
	QueueSize int
	// Timeout is the HTTP request timeout
	Timeout time.Duration

	startOnce sync.Once
}

func NewAsyncTransport(options TransportOptions) *AsyncTransport {
	transport := &AsyncTransport{
		sendQueue:   make(chan *protocol.Envelope, defaultQueueSize),
		workers:     make([]*Worker, defaultWorkerCount),
		workerCount: defaultWorkerCount,
		done:        make(chan struct{}),
		limits:      make(ratelimit.Map),
		QueueSize:   defaultQueueSize,
		Timeout:     defaultTimeout,
		logger:      options.DebugLogger,
	}

	dsn, err := protocol.NewDsn(options.Dsn)
	if err != nil {
		if transport.logger != nil {
			transport.logger.Printf("%v\n", err)
		}
		return transport
	}
	transport.dsn = dsn

	if options.HTTPTransport != nil {
		transport.transport = options.HTTPTransport
	} else {
		transport.transport = &http.Transport{
			Proxy:           getProxyConfig(options),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		transport.client = options.HTTPClient
	} else {
		transport.client = &http.Client{
			Transport: transport.transport,
			Timeout:   transport.Timeout,
		}
	}

	return transport
}

// Start starts the worker goroutines. This method can only be called once.
func (t *AsyncTransport) Start() {
	t.startOnce.Do(func() {
		t.startWorkers()
	})
}

func (t *AsyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	if t.dsn == nil {
		return errors.New("transport not configured")
	}

	select {
	case <-t.done:
		return ErrTransportClosed
	default:
	}

	// Check rate limiting before queuing
	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		return nil // Silently drop rate-limited envelopes
	}

	select {
	case t.sendQueue <- envelope:
		return nil
	default:
		atomic.AddInt64(&t.droppedCount, 1)
		return ErrTransportQueueFull
	}
}

func (t *AsyncTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

func (t *AsyncTransport) FlushWithContext(ctx context.Context) bool {
	// Check if transport is configured
	if t.dsn == nil {
		return true
	}

	flushDone := make(chan struct{})

	go func() {
		defer close(flushDone)

		// First, wait for queue to drain
	drainLoop:
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if len(t.sendQueue) == 0 {
					break drainLoop
				}
				time.Sleep(10 * time.Millisecond)
			}
		}

		// Then wait a bit longer for in-flight requests to complete
		// Since workers process asynchronously, we need to wait for active workers
		time.Sleep(100 * time.Millisecond)
	}()

	select {
	case <-flushDone:
		return true
	case <-ctx.Done():
		return false
	}
}

func (t *AsyncTransport) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	t.mu.Unlock()

	close(t.done)
	close(t.sendQueue)
	t.wg.Wait()
}

// IsRateLimited checks if a specific category is currently rate limited.
func (t *AsyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.isRateLimited(category)
}

func (t *AsyncTransport) startWorkers() {
	for i := 0; i < t.workerCount; i++ {
		worker := &Worker{
			id:        i,
			transport: t,
			done:      t.done,
			wg:        &t.wg,
		}
		t.workers[i] = worker

		t.wg.Add(1)
		go worker.run()
	}
}

func (w *Worker) run() {
	defer w.wg.Done()

	for {
		select {
		case <-w.done:
			return
		case envelope, open := <-w.transport.sendQueue:
			if !open {
				return
			}
			w.processEnvelope(envelope)
		}
	}
}

func (w *Worker) processEnvelope(envelope *protocol.Envelope) {
	maxRetries := defaultMaxRetries
	backoff := defaultRetryBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if w.sendEnvelopeHTTP(envelope) {
			atomic.AddInt64(&w.transport.sentCount, 1)
			return
		}

		if attempt < maxRetries {
			select {
			case <-w.done:
				return
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	atomic.AddInt64(&w.transport.errorCount, 1)
	if w.transport.logger != nil {
		w.transport.logger.Printf("Failed to send envelope after %d attempts", maxRetries+1)
	}
}

func (w *Worker) sendEnvelopeHTTP(envelope *protocol.Envelope) bool {
	// Check rate limiting before processing
	category := categoryFromEnvelope(envelope)
	if w.transport.isRateLimited(category) {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	request, err := getSentryRequestFromEnvelope(ctx, w.transport.dsn, envelope)
	if err != nil {
		if w.transport.logger != nil {
			w.transport.logger.Printf("Failed to create request from envelope: %v", err)
		}
		return false
	}

	response, err := w.transport.client.Do(request)
	if err != nil {
		if w.transport.logger != nil {
			w.transport.logger.Printf("HTTP request failed: %v", err)
		}
		return false
	}
	defer response.Body.Close()

	success := w.handleResponse(response)

	w.transport.mu.Lock()
	w.transport.limits.Merge(ratelimit.FromResponse(response))
	w.transport.mu.Unlock()

	_, _ = io.CopyN(io.Discard, response.Body, maxDrainResponseBytes)

	return success
}

func (w *Worker) handleResponse(response *http.Response) bool {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true
	}

	if response.StatusCode >= 400 && response.StatusCode < 500 {
		if body, err := io.ReadAll(io.LimitReader(response.Body, maxDrainResponseBytes)); err == nil {
			if w.transport.logger != nil {
				w.transport.logger.Printf("Client error %d: %s", response.StatusCode, string(body))
			}
		}
		return false
	}

	if response.StatusCode >= 500 {
		if w.transport.logger != nil {
			w.transport.logger.Printf("Server error %d - will retry", response.StatusCode)
		}
		return false
	}

	if w.transport.logger != nil {
		w.transport.logger.Printf("Unexpected status code %d", response.StatusCode)
	}
	return false
}

func (t *AsyncTransport) isRateLimited(category ratelimit.Category) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	limited := t.limits.IsRateLimited(category)
	if limited {
		if t.logger != nil {
			t.logger.Printf("Rate limited for category %q until %v", category, t.limits.Deadline(category))
		}
	}
	return limited
}
