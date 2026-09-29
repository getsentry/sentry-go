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

const (
	apiVersion = 7

	defaultTimeout           = time.Second * 30
	defaultQueueSize         = 1000
	defaultClientReportsTick = time.Second * 30
)

var (
	// ErrTransportQueueFull indicates that an async envelope could not be queued.
	ErrTransportQueueFull = telemetry.ErrQueueFull
	// ErrTransportClosed indicates that the transport has been closed.
	ErrTransportClosed = errors.New("transport is closed")
	// ErrInvalidEnvelope indicates that an envelope has no header or items.
	ErrInvalidEnvelope = errors.New("invalid envelope: missing header or items")
)

// TransportOptions configures a transport at construction time.
// A transport supplied to ClientOptions.Transport is already configured;
// the client does not apply its HTTP or client-report options to it.
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
	// DisableClientReports disables reports of this transport's losses.
	// Set ClientOptions.DisableClientReports as well to disable reports of
	// client and telemetry-buffer losses when supplying a custom transport.
	DisableClientReports bool
}

func (options TransportOptions) requestTimeout() time.Duration {
	if options.Timeout <= 0 {
		return defaultTimeout
	}
	return options.Timeout
}

func getProxyConfig(httpProxy, httpsProxy string) func(*http.Request) (*url.URL, error) {
	proxy := httpsProxy
	if proxy == "" {
		proxy = httpProxy
	}
	if proxy == "" {
		return http.ProxyFromEnvironment
	}
	proxyURL, err := url.Parse(proxy)
	if err != nil {
		debuglog.Printf("Invalid proxy URL %q: %v", proxy, err)
		return func(*http.Request) (*url.URL, error) { return nil, err }
	}
	return http.ProxyURL(proxyURL)
}

func getTLSConfig(options TransportOptions) *tls.Config {
	if options.CaCerts != nil {
		return &tls.Config{
			RootCAs:    options.CaCerts,
			MinVersion: tls.VersionTLS12,
		}
	}

	return nil
}

// newHTTPClient returns the supplied client or builds one from options.
func newHTTPClient(options TransportOptions) *http.Client {
	if options.HTTPClient != nil {
		return options.HTTPClient
	}
	transport := options.HTTPTransport
	if transport == nil {
		transport = &http.Transport{
			Proxy:           getProxyConfig(options.HTTPProxy, options.HTTPSProxy),
			TLSClientConfig: getTLSConfig(options),
		}
	}
	return &http.Client{Transport: transport, Timeout: options.requestTimeout()}
}

// validEnvelope reports whether envelope can be serialized.
func validEnvelope(envelope *protocol.Envelope) bool {
	if envelope == nil || envelope.Header == nil || len(envelope.Items) == 0 {
		return false
	}
	for _, item := range envelope.Items {
		if item == nil || item.Header == nil {
			return false
		}
	}
	return true
}

func getSentryRequestFromEnvelope(ctx context.Context, dsn *protocol.Dsn, envelope *protocol.Envelope) (*http.Request, error) {
	body, err := envelope.Serialize()
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, dsn.GetAPIURL().String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	var sdkName, sdkVersion string
	if envelope.Header.Sdk != nil {
		sdkVersion = envelope.Header.Sdk.Version
		sdkName = envelope.Header.Sdk.Name
	}
	r.Header.Set("User-Agent", fmt.Sprintf("%s/%s", sdkName, sdkVersion))
	r.Header.Set("Content-Type", "application/x-sentry-envelope")

	auth := fmt.Sprintf("Sentry sentry_version=%d, "+
		"sentry_client=%s/%s, sentry_key=%s", apiVersion, sdkName, sdkVersion, dsn.GetPublicKey())
	if dsn.GetSecretKey() != "" {
		auth = fmt.Sprintf("%s, sentry_secret=%s", auth, dsn.GetSecretKey())
	}
	r.Header.Set("X-Sentry-Auth", auth)
	return r, nil
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
		case protocol.EnvelopeItemTypeTraceMetric:
			return ratelimit.CategoryTraceMetric
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
// SendEnvelope blocks until a response is returned. For most cases, prefer
// httpAsyncTransport.
type httpSyncTransport struct {
	dsn      *protocol.Dsn
	client   *http.Client
	recorder report.ClientReportRecorder
	provider report.ClientReportProvider
	sdkInfo  func() *protocol.SdkInfo
	timeout  time.Duration

	mu     sync.Mutex
	limits ratelimit.Map
}

// NewHTTPSyncTransport creates an HTTP transport without an envelope queue,
// for environments such as serverless platforms that may stop after a
// request. A Client using it sends errors, messages, transactions, and
// check-ins before capture returns. Logs and metrics are still batched, so
// call Client.Flush before exiting.
func NewHTTPSyncTransport(options TransportOptions) Transport {
	recorder, provider := newClientReports(options.DisableClientReports)
	return newHTTPSyncTransport(options, recorder, provider, defaultTransportSDKInfo)
}

func newHTTPSyncTransport(options TransportOptions, recorder report.ClientReportRecorder, provider report.ClientReportProvider, sdkInfo func() *protocol.SdkInfo) Transport {
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

	return &httpSyncTransport{
		timeout:  options.requestTimeout(),
		limits:   make(ratelimit.Map),
		dsn:      dsn,
		client:   newHTTPClient(options),
		recorder: recorder,
		provider: provider,
		sdkInfo:  sdkInfo,
	}
}

func (t *httpSyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()
	return t.sendEnvelope(ctx, envelope)
}

// Synchronous makes the client deliver events during capture.
func (t *httpSyncTransport) Synchronous() {}

func (t *httpSyncTransport) Close() {}

func (t *httpSyncTransport) sendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	if !validEnvelope(envelope) {
		return ErrInvalidEnvelope
	}

	category := categoryFromEnvelope(envelope)
	if t.disabled(category) {
		t.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return nil
	}

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		debuglog.Printf("There was an issue creating the request: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonInternalError, envelope)
		return nil
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
		return nil
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
	ctx, cancel := context.WithTimeout(ctx, t.timeout)
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
	dsn      *protocol.Dsn
	client   *http.Client
	recorder report.ClientReportRecorder
	provider report.ClientReportProvider
	sdkInfo  func() *protocol.SdkInfo

	queue chan *protocol.Envelope

	mu     sync.RWMutex
	limits ratelimit.Map

	// ctx bounds in-flight requests and is canceled by Close.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	flushRequest chan httpFlushRequest
	reporting    bool
	timeout      time.Duration

	closeOnce sync.Once
}

type httpFlushRequest struct {
	ctx  context.Context
	done chan struct{}
}

// NewHTTPTransport creates a fully initialized asynchronous HTTP transport.
// Wrap the returned Transport to intercept envelopes before delivery.
func NewHTTPTransport(options TransportOptions) Transport {
	recorder, provider := newClientReports(options.DisableClientReports)
	return newHTTPTransport(options, recorder, provider, defaultTransportSDKInfo)
}

func newHTTPTransport(options TransportOptions, recorder report.ClientReportRecorder, provider report.ClientReportProvider, sdkInfo func() *protocol.SdkInfo) Transport {
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

	queueSize := defaultQueueSize
	if options.QueueSize > 0 {
		queueSize = options.QueueSize
	}
	transport := &httpAsyncTransport{
		timeout:      options.requestTimeout(),
		reporting:    reporting,
		queue:        make(chan *protocol.Envelope, queueSize),
		flushRequest: make(chan httpFlushRequest),
		done:         make(chan struct{}),
		limits:       make(ratelimit.Map),
		dsn:          dsn,
		client:       newHTTPClient(options),
		recorder:     recorder,
		provider:     provider,
		sdkInfo:      sdkInfo,
	}
	transport.ctx, transport.cancel = context.WithCancel(context.Background())
	transport.wg.Add(1)
	go transport.worker()
	return transport
}

func (t *httpAsyncTransport) SendEnvelope(envelope *protocol.Envelope) error {
	select {
	case <-t.done:
		return ErrTransportClosed
	default:
	}

	if !validEnvelope(envelope) {
		return ErrInvalidEnvelope
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
		case <-t.done:
			return false
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
		t.cancel()
		close(t.done)
		t.wg.Wait()
	})
}

func (t *httpAsyncTransport) worker() {
	defer t.wg.Done()

	var reports <-chan time.Time
	if t.reporting {
		ticker := time.NewTicker(defaultClientReportsTick)
		defer ticker.Stop()
		reports = ticker.C
	}

	for {
		select {
		case <-t.done:
			return
		case <-reports:
			t.sendClientReport()
		case envelope := <-t.queue:
			t.sendEnvelopeHTTP(envelope)
		case flushResponse := <-t.flushRequest:
			t.drainQueue(flushResponse.ctx)
			if flushResponse.ctx.Err() == nil {
				t.sendClientReport()
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
func (t *httpAsyncTransport) sendClientReport() {
	if t.isRateLimited(ratelimit.CategoryAll) {
		return
	}
	envelope := clientReportEnvelope(t.provider, t.dsn, t.sdkInfo)
	if envelope == nil {
		return
	}
	t.sendEnvelopeHTTP(envelope)
}

// drainQueue stops sending when ctx expires. The flush deadline bounds the
// wait, not the request already in flight.
func (t *httpAsyncTransport) drainQueue(ctx context.Context) {
	for ctx.Err() == nil {
		select {
		case envelope := <-t.queue:
			t.sendEnvelopeHTTP(envelope)
		default:
			return
		}
	}
}

func (t *httpAsyncTransport) sendEnvelopeHTTP(envelope *protocol.Envelope) {
	category := categoryFromEnvelope(envelope)
	if t.isRateLimited(category) {
		t.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return
	}

	ctx, cancel := context.WithTimeout(t.ctx, t.timeout)
	defer cancel()

	request, err := getSentryRequestFromEnvelope(ctx, t.dsn, envelope)
	if err != nil {
		debuglog.Printf("Failed to create request from envelope: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonInternalError, envelope)
		return
	}

	identifier := util.EnvelopeIdentifier(envelope)
	result, err := util.DoSendRequest(t.client, request, identifier)
	if err != nil {
		debuglog.Printf("HTTP request failed: %v", err)
		t.recorder.RecordForEnvelope(report.ReasonNetworkError, envelope)
		return
	}
	if result.IsSendError() {
		t.recorder.RecordForEnvelope(report.ReasonSendError, envelope)
	}

	t.mu.Lock()
	t.limits.Merge(result.Limits)
	t.mu.Unlock()
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

func (t *noopEnvelopeTransport) Flush(_ time.Duration) bool {
	return true
}

func (t *noopEnvelopeTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *noopEnvelopeTransport) Close() {
	// Nothing to close
}

func newClientReports(disabled bool) (report.ClientReportRecorder, report.ClientReportProvider) {
	if disabled {
		return report.NoopRecorder(), nil
	}
	aggregator := report.NewAggregator()
	return aggregator, aggregator
}

func defaultTransportSDKInfo() *protocol.SdkInfo {
	return &protocol.SdkInfo{Name: sdkIdentifier, Version: SDKVersion}
}
