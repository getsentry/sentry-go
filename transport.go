package sentry

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/certifi/gocertifi"
)

const defaultBufferSize = 30
const defaultRetryAfter = time.Second * 60

// Transport is used by the `Client` to deliver events to remote server.
type Transport interface {
	Flush(timeout time.Duration) bool
	Configure(options ClientOptions)
	SendEvent(event *Event)
}

// httpTransport is a default implementation of `Transport` interface used by `Client`.
type httpTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport *http.Transport

	buffer        chan *http.Request
	disabledUntil time.Time

	wg    sync.WaitGroup
	start sync.Once
}

// Configure is called by the `Client` itself, providing it it's own `ClientOptions`.
func (t *httpTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		Logger.Printf("%v\n", err)
		return
	}
	t.dsn = dsn

	bufferSize := defaultBufferSize
	if options.BufferSize != 0 {
		bufferSize = options.BufferSize
	}
	t.buffer = make(chan *http.Request, bufferSize)

	if options.HTTPTransport != nil {
		t.transport = options.HTTPTransport
	} else {
		t.transport = &http.Transport{
			Proxy:           t.getProxyConfig(options),
			TLSClientConfig: t.getTLSConfig(options),
		}
	}

	t.client = &http.Client{
		Transport: t.transport,
	}

	t.start.Do(func() {
		go t.worker()
	})
}

// SendEvent assembles a new packet out of `Event` and sends it to remote server.
func (t *httpTransport) SendEvent(event *Event) {
	if t.dsn == nil || time.Now().Before(t.disabledUntil) {
		return
	}

	body, _ := json.Marshal(event)

	request, _ := http.NewRequest(
		http.MethodPost,
		t.dsn.StoreAPIURL().String(),
		bytes.NewBuffer(body),
	)

	for headerKey, headerValue := range t.dsn.RequestHeaders() {
		request.Header.Set(headerKey, headerValue)
	}

	select {
	case t.buffer <- request:
		Logger.Printf(
			"Sending %s event [%s] to %s project: %d\n",
			event.Level,
			event.EventID,
			t.dsn.host,
			t.dsn.projectID,
		)
		t.wg.Add(1)
	default:
		Logger.Println("Event dropped due to transport buffer being full")
		// worker would block, drop the packet
	}
}

// Flush notifies when all the buffered events have been sent by returning `true`
// or `false` if timeout was reached.
func (t *httpTransport) Flush(timeout time.Duration) bool {
	c := make(chan struct{})

	go func() {
		t.wg.Wait()
		close(c)
	}()

	select {
	case <-c:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (t *httpTransport) getProxyConfig(options ClientOptions) func(*http.Request) (*url.URL, error) {
	if options.HTTPSProxy != "" {
		return func(_ *http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPSProxy)
		}
	} else if options.HTTPProxy != "" {
		return func(_ *http.Request) (*url.URL, error) {
			return url.Parse(options.HTTPProxy)
		}
	}

	return http.ProxyFromEnvironment
}

func (t *httpTransport) getTLSConfig(options ClientOptions) *tls.Config {
	if options.CaCerts != nil {
		return &tls.Config{
			RootCAs: options.CaCerts,
		}
	}

	rootCAs, err := gocertifi.CACerts()
	if err != nil {
		Logger.Printf("Coudnt load CA Certificates: %v\n", err)
	}
	return &tls.Config{
		RootCAs: rootCAs,
	}
}

func (t *httpTransport) worker() {
	for request := range t.buffer {
		if time.Now().Before(t.disabledUntil) {
			t.wg.Done()
			continue
		}

		response, err := t.client.Do(request)

		if err != nil {
			Logger.Printf("There was an issue with sending an event: %v", err)
		}

		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			t.disabledUntil = time.Now().Add(retryAfter(time.Now(), response))
			Logger.Printf("Too many requests, backing off till: %s\n", t.disabledUntil)
		}

		t.wg.Done()
	}
}

func retryAfter(now time.Time, r *http.Response) time.Duration {
	retryAfterHeader := r.Header["Retry-After"]

	if retryAfterHeader == nil {
		return defaultRetryAfter
	}

	if date, err := time.Parse(time.RFC1123, retryAfterHeader[0]); err == nil {
		return date.Sub(now)
	}

	if seconds, err := strconv.Atoi(retryAfterHeader[0]); err == nil {
		return time.Second * time.Duration(seconds)
	}

	return defaultRetryAfter
}
