package http

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"go.uber.org/goleak"
)

func testEnvelope(itemType protocol.EnvelopeItemType) *protocol.Envelope {
	return &protocol.Envelope{
		Header: &protocol.EnvelopeHeader{
			EventID: "test-event-id",
			Sdk: &protocol.SdkInfo{
				Name:    "test",
				Version: "1.0.0",
			},
		},
		Items: []*protocol.EnvelopeItem{
			{
				Header: &protocol.EnvelopeItemHeader{
					Type: itemType,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}
}

// nolint:gocyclo
func TestAsyncTransport_SendEnvelope(t *testing.T) {
	t.Run("invalid DSN", func(t *testing.T) {
		transport := NewAsyncTransport(TransportOptions{})

		if _, ok := transport.(*NoopTransport); !ok {
			t.Errorf("expected NoopTransport for empty DSN, got %T", transport)
		}

		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("NoopTransport should not error, got %v", err)
		}
	})

	t.Run("closed transport", func(t *testing.T) {
		tr := NewAsyncTransport(TransportOptions{Dsn: "https://key@sentry.io/123"})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		transport.Close()

		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !errors.Is(err, ErrTransportClosed) {
			t.Errorf("expected ErrTransportClosed, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name     string
			itemType protocol.EnvelopeItemType
		}{
			{"event", protocol.EnvelopeItemTypeEvent},
			{"transaction", protocol.EnvelopeItemTypeTransaction},
			{"check-in", protocol.EnvelopeItemTypeCheckIn},
			{"log", protocol.EnvelopeItemTypeLog},
			{"attachment", protocol.EnvelopeItemTypeAttachment},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		for _, tt := range tests {
			if err := transport.SendEnvelope(testEnvelope(tt.itemType)); err != nil {
				t.Errorf("send %s failed: %v", tt.name, err)
			}
		}

		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		expectedCount := int64(len(tests))
		if sent := atomic.LoadInt64(&transport.sentCount); sent != expectedCount {
			t.Errorf("expected %d sent, got %d", expectedCount, sent)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		if err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
			t.Fatalf("failed to send envelope: %v", err)
		}

		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		if sent := atomic.LoadInt64(&transport.sentCount); sent != 0 {
			t.Errorf("expected 0 sent, got %d", sent)
		}
		if errors := atomic.LoadInt64(&transport.errorCount); errors != 1 {
			t.Errorf("expected 1 error, got %d", errors)
		}
	})

	t.Run("rate limiting by category", func(t *testing.T) {
		var count int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt64(&count, 1) == 1 {
				w.Header().Add("X-Sentry-Rate-Limits", "60:error,60:transaction")
				w.WriteHeader(http.StatusTooManyRequests)
			} else {
				w.WriteHeader(http.StatusOK)
			}
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		if !transport.IsRateLimited(ratelimit.CategoryError) {
			t.Error("error category should be rate limited")
		}
		if !transport.IsRateLimited(ratelimit.CategoryTransaction) {
			t.Error("transaction category should be rate limited")
		}
		if transport.IsRateLimited(ratelimit.CategoryMonitor) {
			t.Error("monitor category should not be rate limited")
		}

		for i := 0; i < 2; i++ {
			_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		}
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}
	})

	t.Run("queue overflow", func(t *testing.T) {
		blockChan := make(chan struct{})
		requestReceived := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			select {
			case requestReceived <- struct{}{}:
			default:
			}
			<-blockChan
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		dsn, _ := protocol.NewDsn("http://key@" + server.URL[7:] + "/123")
		transport := &AsyncTransport{
			QueueSize: 2,
			Timeout:   defaultTimeout,
			done:      make(chan struct{}),
			limits:    make(ratelimit.Map),
			dsn:       dsn,
			transport: &http.Transport{},
			client:    &http.Client{Timeout: defaultTimeout},
		}
		// manually set the queue size to simulate overflow
		transport.queue = make(chan *protocol.Envelope, transport.QueueSize)
		transport.flushRequest = make(chan chan struct{})
		transport.start()
		defer func() {
			close(blockChan)
			transport.Close()
		}()

		if err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
			t.Fatalf("first send should succeed: %v", err)
		}

		<-requestReceived

		for i := 0; i < transport.QueueSize; i++ {
			if err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
				t.Errorf("send %d should succeed: %v", i, err)
			}
		}

		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !errors.Is(err, ErrTransportQueueFull) {
			t.Errorf("expected ErrTransportQueueFull, got %v", err)
		}
	})

	t.Run("FlushMultipleTimes", func(t *testing.T) {
		var count int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&count, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		if err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
			t.Fatalf("failed to send envelope: %v", err)
		}
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		initial := atomic.LoadInt64(&count)
		for i := 0; i < 10; i++ {
			if !transport.Flush(testutils.FlushTimeout()) {
				t.Fatalf("Flush %d timed out", i)
			}
		}

		if got := atomic.LoadInt64(&count); got != initial {
			t.Errorf("expected %d requests after multiple flushes, got %d", initial, got)
		}
	})
}

func TestAsyncTransport_FlushWithContext(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))

		ctx := context.Background()
		if !transport.FlushWithContext(ctx) {
			t.Error("FlushWithContext should succeed")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		tr := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		transport, ok := tr.(*AsyncTransport)
		if !ok {
			t.Fatalf("expected *AsyncTransport, got %T", tr)
		}
		defer transport.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(10 * time.Millisecond)

		if transport.FlushWithContext(ctx) {
			t.Error("FlushWithContext should timeout")
		}
	})
}

func TestAsyncTransport_Close(t *testing.T) {
	tr := NewAsyncTransport(TransportOptions{
		Dsn: "https://key@sentry.io/123",
	})
	transport, ok := tr.(*AsyncTransport)
	if !ok {
		t.Fatalf("expected *AsyncTransport, got %T", tr)
	}

	transport.Close()
	transport.Close()
	transport.Close()

	select {
	case <-transport.done:
	default:
		t.Error("transport should be closed")
	}
}

func TestSyncTransport_SendEnvelope(t *testing.T) {
	t.Run("invalid DSN", func(t *testing.T) {
		transport := NewSyncTransport(TransportOptions{})
		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("invalid DSN should return nil, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name     string
			itemType protocol.EnvelopeItemType
		}{
			{"event", protocol.EnvelopeItemTypeEvent},
			{"transaction", protocol.EnvelopeItemTypeTransaction},
			{"check-in", protocol.EnvelopeItemTypeCheckIn},
			{"log", protocol.EnvelopeItemTypeLog},
			{"attachment", protocol.EnvelopeItemTypeAttachment},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewSyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		for _, tt := range tests {
			if err := transport.SendEnvelope(testEnvelope(tt.itemType)); err != nil {
				t.Errorf("send %s failed: %v", tt.name, err)
			}
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Add("X-Sentry-Rate-Limits", "60:error,60:transaction")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		transport := NewSyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})

		_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))

		if !transport.IsRateLimited(ratelimit.CategoryError) {
			t.Error("error category should be rate limited")
		}
		if !transport.IsRateLimited(ratelimit.CategoryTransaction) {
			t.Error("transaction category should be rate limited")
		}
		if transport.IsRateLimited(ratelimit.CategoryMonitor) {
			t.Error("monitor category should not be rate limited")
		}

		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("rate limited envelope should return nil, got %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal error"))
		}))
		defer server.Close()

		transport := NewSyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})

		err := transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("server error should not return error, got %v", err)
		}
	})
}

func TestSyncTransport_Flush(t *testing.T) {
	transport := NewSyncTransport(TransportOptions{})

	if !transport.Flush(testutils.FlushTimeout()) {
		t.Error("Flush should always succeed")
	}

	if !transport.FlushWithContext(context.Background()) {
		t.Error("FlushWithContext should always succeed")
	}
}

type httptraceRoundTripper struct {
	reusedConn []bool
}

func (rt *httptraceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	trace := &httptrace.ClientTrace{
		GotConn: func(connInfo httptrace.GotConnInfo) {
			rt.reusedConn = append(rt.reusedConn, connInfo.Reused)
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	return http.DefaultTransport.RoundTrip(req)
}

func TestKeepAlive(t *testing.T) {
	tests := []struct {
		name  string
		async bool
	}{
		{"AsyncTransport", true},
		{"SyncTransport", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			largeResponse := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, `{"id":"ec71d87189164e79ab1e61030c183af0"}`)
				if largeResponse {
					fmt.Fprintln(w, strings.Repeat(" ", maxDrainResponseBytes))
				}
			}))
			defer server.Close()

			rt := &httptraceRoundTripper{}
			dsn := "http://key@" + server.URL[7:] + "/123"

			var transport interface {
				SendEnvelope(*protocol.Envelope) error
				Flush(time.Duration) bool
				Close()
			}

			if tt.async {
				tr := NewAsyncTransport(TransportOptions{
					Dsn:           dsn,
					HTTPTransport: rt,
				})
				asyncTransport, ok := tr.(*AsyncTransport)
				if !ok {
					t.Fatalf("expected *AsyncTransport")
				}
				defer asyncTransport.Close()
				transport = asyncTransport
			} else {
				transport = NewSyncTransport(TransportOptions{
					Dsn:           dsn,
					HTTPTransport: rt,
				})
			}

			reqCount := 0
			checkReuse := func(expected bool) {
				t.Helper()
				reqCount++
				if !transport.Flush(testutils.FlushTimeout()) {
					t.Fatal("Flush timed out")
				}
				if len(rt.reusedConn) != reqCount {
					t.Fatalf("got %d requests, want %d", len(rt.reusedConn), reqCount)
				}
				if rt.reusedConn[reqCount-1] != expected {
					t.Fatalf("connection reuse = %v, want %v", rt.reusedConn[reqCount-1], expected)
				}
			}

			_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
			checkReuse(false)

			for i := 0; i < 3; i++ {
				_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
				checkReuse(true)
			}

			largeResponse = true

			_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
			checkReuse(true)

			for i := 0; i < 3; i++ {
				_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
				checkReuse(false)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	tests := []struct {
		name  string
		async bool
	}{
		{"AsyncTransport", true},
		{"SyncTransport", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			dsn := "http://key@" + server.URL[7:] + "/123"

			var transport interface {
				SendEnvelope(*protocol.Envelope) error
				Flush(time.Duration) bool
				Close()
			}

			if tt.async {
				tr := NewAsyncTransport(TransportOptions{Dsn: dsn})
				asyncTransport, ok := tr.(*AsyncTransport)
				if !ok {
					t.Fatalf("expected *AsyncTransport")
				}
				defer asyncTransport.Close()
				transport = asyncTransport
			} else {
				transport = NewSyncTransport(TransportOptions{Dsn: dsn})
			}

			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 5; j++ {
						_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
					}
				}()
			}
			wg.Wait()

			transport.Flush(testutils.FlushTimeout())
		})
	}
}

func TestTransportConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		options  TransportOptions
		async    bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "HTTPProxy",
			options: TransportOptions{
				Dsn:       "https://key@sentry.io/123",
				HTTPProxy: "http://proxy:8080",
			},
			async: true,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*AsyncTransport)
				httpTransport, ok := transport.transport.(*http.Transport)
				if !ok {
					t.Fatal("expected *http.Transport")
				}
				if httpTransport.Proxy == nil {
					t.Fatal("expected proxy function")
				}

				req, _ := http.NewRequest("GET", "https://example.com", nil)
				proxyURL, err := httpTransport.Proxy(req)
				if err != nil {
					t.Fatalf("Proxy function error: %v", err)
				}
				if proxyURL == nil || proxyURL.String() != "http://proxy:8080" {
					t.Errorf("expected proxy URL 'http://proxy:8080', got %v", proxyURL)
				}
			},
		},
		{
			name: "HTTPSProxy",
			options: TransportOptions{
				Dsn:        "https://key@sentry.io/123",
				HTTPSProxy: "https://secure-proxy:8443",
			},
			async: true,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*AsyncTransport)
				httpTransport, ok := transport.transport.(*http.Transport)
				if !ok {
					t.Fatal("expected *http.Transport")
				}

				req, _ := http.NewRequest("GET", "https://example.com", nil)
				proxyURL, err := httpTransport.Proxy(req)
				if err != nil {
					t.Fatalf("Proxy function error: %v", err)
				}
				if proxyURL == nil || proxyURL.String() != "https://secure-proxy:8443" {
					t.Errorf("expected proxy URL 'https://secure-proxy:8443', got %v", proxyURL)
				}
			},
		},
		{
			name: "CustomHTTPTransport",
			options: TransportOptions{
				Dsn:           "https://key@sentry.io/123",
				HTTPTransport: &http.Transport{},
				HTTPProxy:     "http://proxy:8080",
			},
			async: true,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*AsyncTransport)
				if transport.transport.(*http.Transport).Proxy != nil {
					t.Error("custom transport should not have proxy from options")
				}
			},
		},
		{
			name: "CaCerts",
			options: TransportOptions{
				Dsn:     "https://key@sentry.io/123",
				CaCerts: x509.NewCertPool(),
			},
			async: false,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*SyncTransport)
				httpTransport, ok := transport.transport.(*http.Transport)
				if !ok {
					t.Fatal("expected *http.Transport")
				}
				if httpTransport.TLSClientConfig == nil {
					t.Fatal("expected TLS config")
				}
				if httpTransport.TLSClientConfig.RootCAs == nil {
					t.Error("expected custom certificate pool")
				}
			},
		},
		{
			name: "AsyncTransport defaults",
			options: TransportOptions{
				Dsn: "https://key@sentry.io/123",
			},
			async: true,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*AsyncTransport)
				if transport.QueueSize != defaultQueueSize {
					t.Errorf("QueueSize = %d, want %d", transport.QueueSize, defaultQueueSize)
				}
				if transport.Timeout != defaultTimeout {
					t.Errorf("Timeout = %v, want %v", transport.Timeout, defaultTimeout)
				}
			},
		},
		{
			name: "SyncTransport defaults",
			options: TransportOptions{
				Dsn: "https://key@sentry.io/123",
			},
			async: false,
			validate: func(t *testing.T, tr interface{}) {
				transport := tr.(*SyncTransport)
				if transport.Timeout != defaultTimeout {
					t.Errorf("Timeout = %v, want %v", transport.Timeout, defaultTimeout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.async {
				transport := NewAsyncTransport(tt.options)
				defer transport.Close()
				tt.validate(t, transport)
			} else {
				transport := NewSyncTransport(tt.options)
				tt.validate(t, transport)
			}
		})
	}
}

func TestAsyncTransportDoesntLeakGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	tr := NewAsyncTransport(TransportOptions{
		Dsn: "https://test@foobar/1",
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, fmt.Errorf("mock transport")
				},
			},
		},
	})
	transport, ok := tr.(*AsyncTransport)
	if !ok {
		t.Fatalf("expected *AsyncTransport")
	}

	_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
	transport.Flush(testutils.FlushTimeout())
	transport.Close()
}
