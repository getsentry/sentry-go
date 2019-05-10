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

const bufferSize = 5
const defaultRetryAfter = time.Second * 60

type Transport interface {
	Flush(timeout time.Duration) bool
	Configure(options ClientOptions)
	SendEvent(event *Event)
}

type HTTPTransport struct {
	dsn       *Dsn
	client    *http.Client
	transport *http.Transport

	buffer        chan *http.Request
	disabledUntil time.Time

	wg    sync.WaitGroup
	start sync.Once
}

func (t *HTTPTransport) Configure(options ClientOptions) {
	dsn, err := NewDsn(options.Dsn)
	if err != nil {
		debugger.Printf("%v\n", err)
		return
	}
	t.dsn = dsn
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

func (t *HTTPTransport) SendEvent(event *Event) {
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

	debugger.Printf(
		"Sending %s event [%s] to %s project: %d\n",
		event.Level,
		event.EventID,
		t.dsn.host,
		t.dsn.projectID,
	)

	select {
	case t.buffer <- request:
		t.wg.Add(1)
	default:
		// worker would block, drop the packet
	}
}

func (t *HTTPTransport) Flush(timeout time.Duration) bool {
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

func (t *HTTPTransport) getProxyConfig(options ClientOptions) func(*http.Request) (*url.URL, error) {
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

func (t *HTTPTransport) getTLSConfig(options ClientOptions) *tls.Config {
	if options.CaCerts != nil {
		return &tls.Config{
			RootCAs: options.CaCerts,
		}
	}

	rootCAs, err := gocertifi.CACerts()
	if err != nil {
		debugger.Printf("Coudnt load CA Certificates: %v\n", err)
	}
	return &tls.Config{
		RootCAs: rootCAs,
	}
}

func (t *HTTPTransport) worker() {
	for request := range t.buffer {
		if time.Now().Before(t.disabledUntil) {
			t.wg.Done()
			continue
		}

		response, err := t.client.Do(request)

		if err != nil {
			debugger.Printf("There was an issue with sending an event: %v", err)
		}

		if response != nil && response.StatusCode == http.StatusTooManyRequests {
			t.disabledUntil = time.Now().Add(retryAfter(time.Now(), response))
			debugger.Printf("Too many requests, backing off till: %s\n", t.disabledUntil)
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
