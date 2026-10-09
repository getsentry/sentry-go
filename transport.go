package sentry

import (
	"bytes"
	"context"
	"crypto/tls"
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

	defaultTimeout   = time.Second * 30
	defaultQueueSize = 1000
)

var (
	ErrTransportQueueFull = telemetry.ErrQueueFull
	ErrTransportClosed    = errors.New("transport is closed")
	ErrInvalidEnvelope    = errors.New("invalid envelope: missing header or items")
)

// Transport delivers envelopes prepared by the telemetry processor.
// Implementations may send immediately or enqueue envelopes for delivery and
// must be safe for concurrent use.
type Transport interface {
	// Configure initializes the transport before use. NewClient calls it once
	// with resolved options; wrappers must forward it to their transport.
	Configure(options ClientOptions)
	// SendEnvelope returns an error only when rejecting an envelope. The caller
	// records rejected items; built-in transports record losses after acceptance.
	// A nil error does not guarantee delivery. Implementations must return
	// promptly once ctx is done.
	SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error
	// Flush waits for pending delivery attempts up to the given timeout.
	Flush(timeout time.Duration) bool
	// FlushWithContext waits for pending delivery attempts until ctx is canceled.
	FlushWithContext(ctx context.Context) bool
	// Close releases transport resources.
	Close()
}

func getProxyConfig(httpProxy, httpsProxy string) func(*http.Request) (*url.URL, error) {
	if len(httpsProxy) > 0 {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(httpsProxy)
		}
	}

	if len(httpProxy) > 0 {
		return func(*http.Request) (*url.URL, error) {
			return url.Parse(httpProxy)
		}
	}

	return http.ProxyFromEnvironment
}

func getTLSConfig(options ClientOptions) *tls.Config {
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

// markSyncDelivery tells the configuring client that events must be sent inline.
func (o ClientOptions) markSyncDelivery() {
	if o.syncDelivery != nil {
		*o.syncDelivery = true
	}
}

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

// httpSender holds the delivery state shared by SyncTransport and AsyncTransport.
type httpSender struct {
	dsn       *protocol.Dsn
	client    *http.Client
	transport http.RoundTripper
	recorder  report.ClientReportRecorder

	mu     sync.RWMutex
	limits ratelimit.Map
}

func newHTTPSender(dsn *protocol.Dsn, options ClientOptions, timeout time.Duration) *httpSender {
	recorder := options.recorder
	if recorder == nil {
		recorder = report.NoopRecorder()
	}

	sender := &httpSender{
		limits:   make(ratelimit.Map),
		dsn:      dsn,
		recorder: recorder,
	}

	if options.HTTPTransport != nil {
		sender.transport = options.HTTPTransport
	} else {
		sender.transport = &http.Transport{
			Proxy:           getProxyConfig(options.HTTPProxy, options.HTTPSProxy),
			TLSClientConfig: getTLSConfig(options),
		}
	}

	if options.HTTPClient != nil {
		sender.client = options.HTTPClient
	} else {
		sender.client = &http.Client{
			Transport: sender.transport,
			Timeout:   timeout,
		}
	}

	return sender
}

// SyncTransport is a blocking implementation of Transport.
//
// Clients using this transport will send requests to Sentry and
// block until a response is returned.
//
// The blocking behavior is useful in a limited set of use cases. For example,
// use it when deploying code to a Function as a Service ("Serverless")
// platform, where any work happening in a background goroutine is not
// guaranteed to execute.
//
// For most cases, prefer AsyncTransport.
type SyncTransport struct {
	*httpSender

	// ctx bounds in-flight requests and is canceled by Close.
	ctx    context.Context
	cancel context.CancelFunc

	Timeout time.Duration
}

// NewSyncTransport creates a blocking HTTP transport.
func NewSyncTransport() *SyncTransport {
	return &SyncTransport{Timeout: defaultTimeout}
}

// Configure initializes the transport with the client's options before use.
func (t *SyncTransport) Configure(options ClientOptions) {
	options.markSyncDelivery()
	t.ctx, t.cancel = context.WithCancel(context.Background())
	dsn, err := protocol.NewDsn(options.Dsn)
	t.httpSender = newHTTPSender(dsn, options, t.Timeout)
	if err != nil || dsn == nil {
		debuglog.Printf("Transport is disabled: invalid dsn: %v", err)
	}
}

// Close cancels in-flight requests and rejects later sends.
func (t *SyncTransport) Close() {
	if t.cancel != nil {
		t.cancel()
	}
}

func (t *SyncTransport) SendEnvelope(ctx context.Context, envelope *protocol.Envelope) error {
	if t.httpSender == nil || t.dsn == nil {
		return nil
	}
	if t.ctx.Err() != nil {
		return ErrTransportClosed
	}
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	defer context.AfterFunc(t.ctx, cancel)()

	if !validEnvelope(envelope) {
		return ErrInvalidEnvelope
	}

	debuglog.Printf(
		"Sending %s to %s project: %s",
		util.EnvelopeIdentifier(envelope),
		t.dsn.GetHost(),
		t.dsn.GetProjectID(),
	)
	t.send(ctx, envelope)
	return nil
}

func (t *SyncTransport) Flush(_ time.Duration) bool {
	return true
}

func (t *SyncTransport) FlushWithContext(_ context.Context) bool {
	return true
}

// AsyncTransport is the default, non-blocking, implementation of Transport.
//
// Clients using this transport will enqueue requests in a queue and return to
// the caller before any network communication has happened. Requests are sent
// to Sentry sequentially from a background goroutine.
type AsyncTransport struct {
	*httpSender

	queue chan *protocol.Envelope

	// ctx bounds in-flight requests and is canceled by Close.
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup

	flushRequest chan flushRequest

	closeMu sync.RWMutex

	QueueSize int
	Timeout   time.Duration

	startOnce sync.Once
	closeOnce sync.Once
}

type flushRequest struct {
	ctx  context.Context
	done chan struct{}
}

// NewAsyncTransport creates an asynchronous HTTP transport with a bounded queue.
// NewClient configures it before use.
func NewAsyncTransport() *AsyncTransport {
	return &AsyncTransport{
		QueueSize: defaultQueueSize,
		Timeout:   defaultTimeout,
	}
}

// Configure initializes the transport with the client's options before use.
func (t *AsyncTransport) Configure(options ClientOptions) {
	t.done = make(chan struct{})
	dsn, err := protocol.NewDsn(options.Dsn)
	t.httpSender = newHTTPSender(dsn, options, t.Timeout)
	if err != nil || dsn == nil {
		debuglog.Printf("Transport is disabled: invalid dsn: %v", err)
		return
	}
	t.queue = make(chan *protocol.Envelope, t.QueueSize)
	t.flushRequest = make(chan flushRequest)
	t.start()
}

func (t *AsyncTransport) start() {
	t.startOnce.Do(func() {
		if t.recorder == nil {
			t.recorder = report.NoopRecorder()
		}
		t.ctx, t.cancel = context.WithCancel(context.Background())
		t.wg.Add(1)
		go t.worker()
	})
}

// SendEnvelope enqueues envelope without waiting for queue space.
func (t *AsyncTransport) SendEnvelope(_ context.Context, envelope *protocol.Envelope) error {
	if t.httpSender == nil || t.dsn == nil {
		return nil
	}
	t.closeMu.RLock()
	defer t.closeMu.RUnlock()

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

func (t *AsyncTransport) Flush(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return t.FlushWithContext(ctx)
}

func (t *AsyncTransport) FlushWithContext(ctx context.Context) bool {
	if t.httpSender == nil || t.dsn == nil {
		return true
	}
	t.closeMu.RLock()
	defer t.closeMu.RUnlock()

	if ctx.Err() != nil || t.ctx.Err() != nil {
		return false
	}
	flushResponse := make(chan struct{})
	select {
	case <-t.ctx.Done():
		debuglog.Println("Failed to flush, transport is closed.")
		return false
	case t.flushRequest <- flushRequest{ctx: ctx, done: flushResponse}:
		select {
		case <-flushResponse:
			if ctx.Err() != nil || t.ctx.Err() != nil {
				return false
			}
			debuglog.Println("Buffer flushed successfully.")
			return true
		case <-t.ctx.Done():
			debuglog.Println("Failed to flush, transport is closed.")
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

func (t *AsyncTransport) Close() {
	t.closeOnce.Do(func() {
		if t.cancel != nil {
			t.cancel()
		}
		t.closeMu.Lock()
		defer t.closeMu.Unlock()

		if t.done != nil {
			close(t.done)
		}
		t.wg.Wait()
	})
}

func (t *AsyncTransport) worker() {
	defer t.wg.Done()

	for t.ctx.Err() == nil {
		select {
		case <-t.ctx.Done():
			return
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.sendEnvelopeHTTP(envelope)
		case flushRequest, open := <-t.flushRequest:
			if !open {
				return
			}
			t.drainQueue(flushRequest.ctx)
			close(flushRequest.done)
		}
	}
}

func (t *AsyncTransport) drainQueue(ctx context.Context) {
	for ctx.Err() == nil && t.ctx.Err() == nil {
		select {
		case envelope, open := <-t.queue:
			if !open {
				return
			}
			t.sendEnvelopeHTTP(envelope)
		default:
			return
		}
	}
}

func (t *AsyncTransport) sendEnvelopeHTTP(envelope *protocol.Envelope) bool { //nolint: unparam
	ctx, cancel := context.WithTimeout(t.ctx, t.Timeout)
	defer cancel()
	return t.send(ctx, envelope)
}

// send records every loss of an accepted envelope.
func (s *httpSender) send(ctx context.Context, envelope *protocol.Envelope) bool {
	category := categoryFromEnvelope(envelope)
	if s.isRateLimited(category) {
		s.recorder.RecordForEnvelope(report.ReasonRateLimitBackoff, envelope)
		return false
	}
	request, err := getSentryRequestFromEnvelope(ctx, s.dsn, envelope)
	if err != nil {
		debuglog.Printf("Failed to create request from envelope: %v", err)
		s.recorder.RecordForEnvelope(report.ReasonInternalError, envelope)
		return false
	}

	identifier := util.EnvelopeIdentifier(envelope)
	result, err := util.DoSendRequest(s.client, request, identifier)
	if err != nil {
		debuglog.Printf("HTTP request failed: %v", err)
		s.recorder.RecordForEnvelope(report.ReasonNetworkError, envelope)
		return false
	}
	if result.IsSendError() {
		s.recorder.RecordForEnvelope(report.ReasonSendError, envelope)
	}

	s.mu.Lock()
	s.limits.Merge(result.Limits)
	s.mu.Unlock()

	return result.Success
}

func (s *httpSender) isRateLimited(category ratelimit.Category) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limited := s.limits.IsRateLimited(category)
	if limited {
		debuglog.Printf("Rate limited for category %q until %v", category, s.limits.Deadline(category))
	}
	return limited
}

// NoopTransport is a transport implementation that drops all events.
// Used internally when an empty or invalid DSN is provided.
type NoopTransport struct{}

func NewNoopTransport() *NoopTransport {
	debuglog.Println("Transport initialized with invalid DSN. Using NoopTransport. No events will be delivered.")
	return &NoopTransport{}
}

func (t *NoopTransport) Configure(_ ClientOptions) {}

func (t *NoopTransport) SendEnvelope(_ context.Context, _ *protocol.Envelope) error {
	debuglog.Println("Envelope dropped due to NoopTransport usage.")
	return nil
}

func (t *NoopTransport) Flush(_ time.Duration) bool {
	return true
}

func (t *NoopTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (t *NoopTransport) Close() {
	// Nothing to close
}
