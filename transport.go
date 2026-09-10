package sentry

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/telemetry"
	"github.com/getsentry/sentry-go/internal/util"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
)

// TransportOptions configures a transport at construction time.
type TransportOptions struct {
	// Dsn is the destination for this transport. An empty or invalid DSN
	// creates a no-op transport. Environment variables are not consulted.
	Dsn string
	// HTTPClient takes precedence over HTTPTransport, proxies, and CaCerts.
	// The supplied client is not modified.
	HTTPClient *http.Client
	// HTTPTransport takes precedence over proxies and CaCerts.
	HTTPTransport http.RoundTripper
	// HTTPProxy overrides the proxy used for outgoing requests.
	HTTPProxy string
	// HTTPSProxy takes precedence over HTTPProxy.
	HTTPSProxy string
	// CaCerts supplies trusted roots for the SDK-created HTTP transport.
	CaCerts *x509.CertPool
	// QueueSize is the async envelope queue capacity. Nonpositive values
	// default to 1000. The synchronous transport ignores this option.
	QueueSize int
	// Timeout bounds each HTTP request. Nonpositive values default to 30s.
	// A supplied HTTPClient's timeout may impose a shorter limit.
	Timeout time.Duration
}

var (
	// ErrTransportQueueFull indicates that an async envelope could not be queued.
	ErrTransportQueueFull = errors.New("transport queue full")
	// ErrTransportClosed indicates that the transport has been closed.
	ErrTransportClosed = errors.New("transport is closed")
	// ErrEmptyEnvelope indicates that an envelope contains no items.
	ErrEmptyEnvelope = errors.New("empty envelope provided")
)

const (
	httpAPIVersion = 7

	defaultHTTPTimeout           = time.Second * 30
	defaultHTTPQueueSize         = 1000
	defaultHTTPClientReportsTick = time.Second * 30
)

func (options TransportOptions) requestTimeout() time.Duration {
	if options.Timeout <= 0 {
		return defaultHTTPTimeout
	}
	return options.Timeout
}

func httpProxyConfig(options TransportOptions) func(*http.Request) (*url.URL, error) {
	if len(options.HTTPSProxy) > 0 {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPSProxy)
		}
	}

	if len(options.HTTPProxy) > 0 {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPProxy)
		}
	}

	return http.ProxyFromEnvironment
}

func httpTLSConfig(options TransportOptions) *tls.Config {
	if options.CaCerts != nil {
		return &tls.Config{
			RootCAs:    options.CaCerts,
			MinVersion: tls.VersionTLS12,
		}
	}

	return nil
}

func getSentryRequestFromEnvelope(ctx context.Context, dsn *protocol.Dsn, envelope *protocol.Envelope) (r *http.Request, err error) {
	defer func() {
		if r != nil {
			var sdkName, sdkVersion string
			if envelope.Header != nil && envelope.Header.Sdk != nil {
				sdkVersion = envelope.Header.Sdk.Version
				sdkName = envelope.Header.Sdk.Name
			}

			r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkName, sdkVersion))
			r.Header.Set("Content-Type", "application/x-sentry-envelope")

			auth := fmt.Sprintf("Sentry sentry_version=%d, "+
				"sentry_client=%s/%s, sentry_key=%s", httpAPIVersion, sdkName, sdkVersion, dsn.GetPublicKey())

			if dsn.GetSecretKey() != "" {
				auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
			}

			r.Header.Set("X-Sentry-Auth", auth)
		}
	}()

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
		case protocol.EnvelopeItemTypeAttachment, protocol.EnvelopeItemTypeClientReport:
			continue
		default:
			return ratelimit.CategoryAll
		}
	}

	return ratelimit.CategoryAll
}

// httpSyncTransport is a blocking implementation of Transport.
//
// SendEnvelope blocks until a response is returned. Clients still buffer
// telemetry before handing it to this transport and must flush before exiting.
//
// For most cases, prefer httpAsyncTransport.
type httpSyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	recorder  report.ClientReportRecorder
	provider  report.ClientReportProvider
	sdkInfo   func() *protocol.SdkInfo

	mu     sync.Mutex
	limits ratelimit.Map

	Timeout time.Duration
}

func newHTTPSyncTransport(options TransportOptions, recorder report.ClientReportRecorder, provider report.ClientReportProvider, sdkInfo func() *protocol.SdkInfo) telemetry.Transport {
	dsn, err := protocol.NewDsn(options.Dsn)
	if err != nil || dsn == nil {
		debuglog.Printf("Transport is disabled: invalid dsn: %v\n", err)
		return newNoopEnvelopeTransport()
	}

	if recorder == nil {
		recorder = report.NoopRecorder()
	}
	if provider == nil {
		provider = report.NoopProvider()
	}

	transport := &httpSyncTransport{
		Timeout:  options.requestTimeout(),
		limits:   make(ratelimit.Map),
		dsn:      dsn,
		recorder: recorder,
		provider: provider,
		sdkInfo:  sdkInfo,
	}

	if options.HTTPTransport != nil {
		transport.transport = options.HTTPTransport
	} else {
		transport.transport = &http.Transport{
			Proxy:           httpProxyConfig(options),
			TLSClientConfig: httpTLSConfig(options),
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

func (t *httpSyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	return t.SendEnvelopeWithContext(context.Background(), envelope)
}

func (t *httpSyncTransport) Close() {}

func (t *httpSyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.disabled(category)
}

func (t *httpSyncTransport) HasCapacity() bool { return true }

func (t *httpSyncTransport) SendEnvelopeWithContext(ctx context.Context, envelope *protocol.Envelope) error {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	return t.sendEnvelope(ctx, envelope)
}

func (t *httpSyncTransport) sendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	if envelope == nil || len(envelope.Items) == 0 {
		return ErrEmptyEnvelope
	}

	category := categoryFromEnvelope(envelope)
	if t.disabled(category) {
		t.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return nil
	}
	// the sync transport needs to attach client reports when available
	t.provider.AttachToEnvelope(envelope)

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		debuglog.Printf("There was an issue creating the request: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonInternalError, envelope)
		return err
	}
	identifier := util.EnvelopeIdentifier(envelope)
	debuglog.Printf(
		"Sending %s to %s project: %s",
		identifier,
		t.dsn.GetHost(),
		t.dsn.GetProjectID(),
	)

	result, err := util.DoSendRequest(t.client, request, identifier)
	if err != nil {
		debuglog.Printf("There was an issue with sending an event: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonNetworkError, envelope)
		return err
	}
	if result.IsSendError() {
		t.recorder.RecordForEnvelope(report.ReasonSendError, envelope)
	}

	t.mu.Lock()
	t.limits.Merge(result.Limits)
	t.mu.Unlock()

	return nil
}

func (t *httpSyncTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

func (t *httpSyncTransport) FlushWithContext(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	if !t.disabled(ratelimit.CategoryAll) {
		if envelope := clientReportEnvelope(t.provider, t.dsn, t.sdkInfo); envelope != nil {
			_ = t.sendEnvelope(ctx, envelope)
		}
	}
	return ctx.Err() == nil
}

func (t *httpSyncTransport) disabled(c ratelimit.Category) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	disabled := t.limits.IsRateLimited(c)
	if disabled {
		debuglog.Printf("Too many requests for %q, backing off till: %v", c, t.limits.Deadline(c))
	}
	return disabled
}

// httpAsyncTransport is the default, non-blocking, implementation of Transport.
//
// Clients using this transport will enqueue requests in a queue and return to
// the caller before any network communication has happened. Requests are sent
// to Sentry sequentially from a background goroutine.
type httpAsyncTransport struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	recorder  report.ClientReportRecorder
	provider  report.ClientReportProvider
	sdkInfo   func() *protocol.SdkInfo

	queue chan *protocol.Envelope

	mu     sync.RWMutex
	limits ratelimit.Map

	done chan struct{}
	wg   sync.WaitGroup

	flushRequest chan httpFlushRequest
	reporting    bool

	closeMu sync.RWMutex

	QueueSize int
	Timeout   time.Duration

	startOnce sync.Once
	closeOnce sync.Once
}

type httpFlushRequest struct {
	ctx  context.Context
	done chan struct{}
}

func newHTTPTransport(options TransportOptions, recorder report.ClientReportRecorder, provider report.ClientReportProvider, sdkInfo func() *protocol.SdkInfo) telemetry.Transport {
	reporting := provider != nil
	dsn, err := protocol.NewDsn(options.Dsn)
	if err != nil || dsn == nil {
		debuglog.Printf("Transport is disabled: invalid dsn: %v", err)
		return newNoopEnvelopeTransport()
	}

	if recorder == nil {
		recorder = report.NoopRecorder()
	}
	if provider == nil {
		provider = report.NoopProvider()
	}

	transport := &httpAsyncTransport{
		QueueSize: defaultHTTPQueueSize,
		Timeout:   options.requestTimeout(),
		reporting: reporting,
		done:      make(chan struct{}),
		limits:    make(ratelimit.Map),
		dsn:       dsn,
		recorder:  recorder,
		provider:  provider,
		sdkInfo:   sdkInfo,
	}

	if options.QueueSize > 0 {
		transport.QueueSize = options.QueueSize
	}
	transport.queue = make(chan *protocol.Envelope, transport.QueueSize)
	transport.flushRequest = make(chan httpFlushRequest)

	if options.HTTPTransport != nil {
		transport.transport = options.HTTPTransport
	} else {
		transport.transport = &http.Transport{
			Proxy:           httpProxyConfig(options),
			TLSClientConfig: httpTLSConfig(options),
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

	transport.start()
	return transport
}

func (t *httpAsyncTransport) start() {
	t.startOnce.Do(func() {
		if t.recorder == nil {
			t.recorder = report.NoopRecorder()
		}
		if t.provider == nil {
			t.provider = report.NoopProvider()
		}
		t.wg.Add(1)
		go t.worker()
	})
}

// HasCapacity reports whether the async transport queue appears to have space
// for at least one more envelope. This is a best-effort, non-blocking check.
func (t *httpAsyncTransport) HasCapacity() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	select {
	case <-t.done:
		return false
	default:
	}
	return len(t.queue) < cap(t.queue)
}

func (t *httpAsyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	t.closeMu.RLock()
	defer t.closeMu.RUnlock()

	select {
	case <-t.done:
		return ErrTransportClosed
	default:
	}

	if envelope == nil || len(envelope.Items) == 0 {
		return ErrEmptyEnvelope
	}

	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		t.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return nil
	}

	identifier := util.EnvelopeIdentifier(envelope)

	select {
	case <-t.done:
		return ErrTransportClosed
	case t.queue <- envelope:
		debuglog.Printf(
			"Sending %s to %s project: %s",
			identifier,
			t.dsn.GetHost(),
			t.dsn.GetProjectID(),
		)
		return nil
	default:
		t.recorder.RecordForEnvelope(report.ReasonQueueOverflow, envelope)
		return ErrTransportQueueFull
	}
}

func (t *httpAsyncTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

func (t *httpAsyncTransport) FlushWithContext(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	t.closeMu.RLock()
	defer t.closeMu.RUnlock()

	flushResponse := make(chan struct{})
	select {
	case <-t.done:
		debuglog.Println("Failed to flush, transport is closed.")
		return false
	case t.flushRequest <- httpFlushRequest{ctx: ctx, done: flushResponse}:
		select {
		case <-flushResponse:
			debuglog.Println("Buffer flushed successfully.")
			return ctx.Err() == nil
		case <-ctx.Done():
			debuglog.Println("Failed to flush, buffer timed out.")
			return false
		}
	case <-ctx.Done():
		debuglog.Println("Failed to flush, buffer timed out.")
		return false
	}
}

func (t *httpAsyncTransport) Close() {
	t.closeOnce.Do(func() {
		t.closeMu.Lock()
		defer t.closeMu.Unlock()

		close(t.done)
		t.wg.Wait()
	})
}

func (t *httpAsyncTransport) IsRateLimited(category ratelimit.Category) bool {
	return t.isRateLimited(category)
}

func (t *httpAsyncTransport) worker() {
	defer t.wg.Done()

	var reports <-chan time.Time
	if t.reporting {
		ticker := time.NewTicker(defaultHTTPClientReportsTick)
		defer ticker.Stop()
		reports = ticker.C
	}

	for {
		select {
		case <-t.done:
			return
		case <-reports:
			t.sendClientReport(context.Background())
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.sendEnvelopeHTTP(envelope)
		case flushResponse, open := <-t.flushRequest:
			if !open {
				return
			}
			t.drainQueue(flushResponse.ctx)
			if flushResponse.ctx.Err() == nil {
				t.sendClientReport(flushResponse.ctx)
			}
			close(flushResponse.done)
		}
	}
}

func clientReportEnvelope(provider report.ClientReportProvider, dsn *protocol.Dsn, sdkInfo func() *protocol.SdkInfo) *protocol.Envelope {
	if provider == nil {
		return nil
	}
	r := provider.TakeReport()
	if r == nil {
		return nil
	}
	item, err := r.ToEnvelopeItem()
	if err != nil {
		debuglog.Printf("Failed to serialize client report: %v", err)
		return nil
	}
	header := &protocol.EnvelopeHeader{Dsn: dsn, SentAt: time.Now()}
	if sdkInfo != nil {
		header.Sdk = sdkInfo()
	}
	return protocol.NewEnvelope(header, item)
}

// sendClientReport sends a standalone report without counting report failures.
func (t *httpAsyncTransport) sendClientReport(ctx context.Context) {
	if t.isRateLimited(ratelimit.CategoryAll) {
		return
	}
	envelope := clientReportEnvelope(t.provider, t.dsn, t.sdkInfo)
	if envelope == nil {
		return
	}
	t.sendEnvelopeHTTPWithContext(ctx, envelope)
}

func (t *httpAsyncTransport) drainQueue(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.sendEnvelopeHTTPWithContext(ctx, envelope)
		default:
			return
		}
	}
}

func (t *httpAsyncTransport) sendEnvelopeHTTP(envelope *protocol.Envelope) bool {
	return t.sendEnvelopeHTTPWithContext(context.Background(), envelope)
}

func (t *httpAsyncTransport) sendEnvelopeHTTPWithContext(ctx context.Context, envelope *protocol.Envelope) bool {
	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		t.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return false
	}
	// attach to envelope after rate-limit check
	t.provider.AttachToEnvelope(envelope)

	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		debuglog.Printf("Failed to create request from envelope: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonInternalError, envelope)
		return false
	}

	identifier := util.EnvelopeIdentifier(envelope)
	result, err := util.DoSendRequest(t.client, request, identifier)
	if err != nil {
		debuglog.Printf("HTTP request failed: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonNetworkError, envelope)
		return false
	}
	if result.IsSendError() {
		t.recorder.RecordForEnvelope(report.ReasonSendError, envelope)
	}

	t.mu.Lock()
	t.limits.Merge(result.Limits)
	t.mu.Unlock()

	return result.Success
}

func (t *httpAsyncTransport) isRateLimited(category ratelimit.Category) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	limited := t.limits.IsRateLimited(category)
	if limited {
		debuglog.Printf("Rate limited for category %q until %v", category, t.limits.Deadline(category))
	}
	return limited
}

// noopEnvelopeTransport is a transport implementation that drops all events.
// Used internally when an empty or invalid DSN is provided.
type noopEnvelopeTransport struct{}

func newNoopEnvelopeTransport() *noopEnvelopeTransport {
	debuglog.Println("Transport initialized with invalid DSN. Using NoopTransport. No events will be delivered.")
	return &noopEnvelopeTransport{}
}

func (t *noopEnvelopeTransport) SendEnvelope(_ *protocol.Envelope) error {
	debuglog.Println("Envelope dropped due to NoopTransport usage.")
	return nil
}

func (t *noopEnvelopeTransport) IsRateLimited(_ ratelimit.Category) bool {
	return false
}

func (t *noopEnvelopeTransport) Flush(_ time.Duration) bool {
	return true
}

func (t *noopEnvelopeTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *noopEnvelopeTransport) Close() {
	// Nothing to close
}

func (t *noopEnvelopeTransport) HasCapacity() bool { return true }
