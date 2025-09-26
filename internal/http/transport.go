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

	defaultQueueSize      = 1000
	defaultRequestTimeout = 30 * time.Second
	defaultMaxRetries     = 3
	defaultRetryBackoff   = time.Second
)

const maxDrainResponseBytes = 16 << 10

var (
	ErrTransportQueueFull = errors.New("transport queue full")
	ErrTransportClosed    = errors.New("transport is closed")
)

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
			sdkName := envelope.Header.Sdk.Name
			sdkVersion := envelope.Header.Sdk.Version

			r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkName, sdkVersion))
			r.Header.Set("Content-Type", "application/x-sentry-envelope")

			auth := fmt.Sprintf("Sentry sentry_version=%d, "+
				"sentry_client=%s/%s, sentry_key=%s", apiVersion, sdkName, sdkVersion, dsn.GetPublicKey())

			if dsn.GetSecretKey() != "" {
				auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
			}

			r.Header.Set("X-Sentry-Auth", auth)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}

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

func categoryFromEnvelope(envelope *protocol.Envelope) ratelimit.Category {
	if envelope == nil || len(envelope.Items) == 0 {
		return ratelimit.CategoryAll
	}

	for _, item := range envelope.Items {
		if item == nil || item.Header == nil {
			continue
		}

		switch item.Header.Type {
		case protocol.EnvelopeItemTypeEvent:
			return ratelimit.CategoryError
		case protocol.EnvelopeItemTypeTransaction:
			return ratelimit.CategoryTransaction
		case protocol.EnvelopeItemTypeCheckIn:
			return ratelimit.CategoryMonitor
		case protocol.EnvelopeItemTypeLog:
			return ratelimit.CategoryLog
		case protocol.EnvelopeItemTypeAttachment:
			continue
		default:
			return ratelimit.CategoryAll
		}
	}

	return ratelimit.CategoryAll
}

type SyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	logger    *log.Logger

	mu     sync.Mutex
	limits ratelimit.Map

	Timeout time.Duration
}

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

func (t *SyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	return t.SendEnvelopeWithContext(context.Background(), envelope)
}

func (t *SyncTransport) Close() {}

func (t *SyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.disabled(category)
}

func (t *SyncTransport) SendEnvelopeWithContext(ctx context.Context, envelope *protocol.Envelope) error {
	if t.dsn == nil {
		return nil
	}

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

	_, _ = io.CopyN(io.Discard, response.Body, maxDrainResponseBytes)
	return response.Body.Close()
}

func (t *SyncTransport) Flush(_ time.Duration) bool {
	return true
}

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

type AsyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	logger    *log.Logger

	queue chan *protocol.Envelope

	mu     sync.RWMutex
	limits ratelimit.Map

	done chan struct{}
	wg   sync.WaitGroup

	flushRequest chan chan struct{}

	sentCount    int64
	droppedCount int64
	errorCount   int64

	QueueSize int
	Timeout   time.Duration

	startOnce sync.Once
	closeOnce sync.Once
}

func NewAsyncTransport(options TransportOptions) *AsyncTransport {
	transport := &AsyncTransport{
		QueueSize: defaultQueueSize,
		Timeout:   defaultTimeout,
		done:      make(chan struct{}),
		limits:    make(ratelimit.Map),
		logger:    options.DebugLogger,
	}

	transport.queue = make(chan *protocol.Envelope, transport.QueueSize)
	transport.flushRequest = make(chan chan struct{})

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

func (t *AsyncTransport) Start() {
	t.startOnce.Do(func() {
		t.wg.Add(1)
		go t.worker()
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

	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		return nil
	}

	select {
	case t.queue <- envelope:
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
	if t.dsn == nil {
		return true
	}

	flushResponse := make(chan struct{})
	select {
	case t.flushRequest <- flushResponse:
		select {
		case <-flushResponse:
			return true
		case <-ctx.Done():
			return false
		}
	case <-ctx.Done():
		return false
	}
}

func (t *AsyncTransport) Close() {
	t.closeOnce.Do(func() {
		close(t.done)
		close(t.queue)
		close(t.flushRequest)
		t.wg.Wait()
	})
}

func (t *AsyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.isRateLimited(category)
}

func (t *AsyncTransport) worker() {
	defer t.wg.Done()

	for {
		select {
		case <-t.done:
			return
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.processEnvelope(envelope)
		case flushResponse, open := <-t.flushRequest:
			if !open {
				return
			}
			t.drainQueue()
			close(flushResponse)
		}
	}
}

func (t *AsyncTransport) drainQueue() {
	for {
		select {
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.processEnvelope(envelope)
		default:
			return
		}
	}
}

func (t *AsyncTransport) processEnvelope(envelope *protocol.Envelope) {
	maxRetries := defaultMaxRetries
	backoff := defaultRetryBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if t.sendEnvelopeHTTP(envelope) {
			atomic.AddInt64(&t.sentCount, 1)
			return
		}

		if attempt < maxRetries {
			select {
			case <-t.done:
				return
			case <-time.After(backoff):
				backoff *= 2
			}
		}
	}

	atomic.AddInt64(&t.errorCount, 1)
	if t.logger != nil {
		t.logger.Printf("Failed to send envelope after %d attempts", maxRetries+1)
	}
}

func (t *AsyncTransport) sendEnvelopeHTTP(envelope *protocol.Envelope) bool {
	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultRequestTimeout)
	defer cancel()

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		if t.logger != nil {
			t.logger.Printf("Failed to create request from envelope: %v", err)
		}
		return false
	}

	response, err := t.client.Do(request)
	if err != nil {
		if t.logger != nil {
			t.logger.Printf("HTTP request failed: %v", err)
		}
		return false
	}
	defer response.Body.Close()

	success := t.handleResponse(response)

	t.mu.Lock()
	if t.limits == nil {
		t.limits = make(ratelimit.Map)
	}
	t.limits.Merge(ratelimit.FromResponse(response))
	t.mu.Unlock()

	_, _ = io.CopyN(io.Discard, response.Body, maxDrainResponseBytes)
	return success
}

func (t *AsyncTransport) handleResponse(response *http.Response) bool {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true
	}

	if response.StatusCode >= 400 && response.StatusCode < 500 {
		if body, err := io.ReadAll(io.LimitReader(response.Body, maxDrainResponseBytes)); err == nil {
			if t.logger != nil {
				t.logger.Printf("Client error %d: %s", response.StatusCode, string(body))
			}
		}
		return false
	}

	if response.StatusCode >= 500 {
		if t.logger != nil {
			t.logger.Printf("Server error %d - will retry", response.StatusCode)
		}
		return false
	}

	if t.logger != nil {
		t.logger.Printf("Unexpected status code %d", response.StatusCode)
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
