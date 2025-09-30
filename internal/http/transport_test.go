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

type mockEnvelopeConvertible struct {
	envelope *protocol.Envelope
	err      error
}

func (m *mockEnvelopeConvertible) ToEnvelope(_ *protocol.Dsn) (*protocol.Envelope, error) {
	return m.envelope, m.err
}

func testTransportOptions(dsn string) TransportOptions {
	return TransportOptions{
		Dsn: dsn,
	}
}

func TestCategoryFromEnvelope(t *testing.T) {
	tests := []struct {
		name     string
		envelope *protocol.Envelope
		expected ratelimit.Category
	}{
		{
			name:     "nil envelope",
			envelope: nil,
			expected: ratelimit.CategoryAll,
		},
		{
			name: "empty envelope",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items:  []*protocol.EnvelopeItem{},
			},
			expected: ratelimit.CategoryAll,
		},
		{
			name: "error event",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeEvent,
						},
					},
				},
			},
			expected: ratelimit.CategoryError,
		},
		{
			name: "transaction event",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeTransaction,
						},
					},
				},
			},
			expected: ratelimit.CategoryTransaction,
		},
		{
			name: "check-in event",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeCheckIn,
						},
					},
				},
			},
			expected: ratelimit.CategoryMonitor,
		},
		{
			name: "log event",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeLog,
						},
					},
				},
			},
			expected: ratelimit.CategoryLog,
		},
		{
			name: "attachment only (skipped)",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeAttachment,
						},
					},
				},
			},
			expected: ratelimit.CategoryAll,
		},
		{
			name: "attachment with error event",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeAttachment,
						},
					},
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeEvent,
						},
					},
				},
			},
			expected: ratelimit.CategoryError,
		},
		{
			name: "unknown item type",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemType("unknown"),
						},
					},
				},
			},
			expected: ratelimit.CategoryAll,
		},
		{
			name: "nil item",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					nil,
				},
			},
			expected: ratelimit.CategoryAll,
		},
		{
			name: "nil item header",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					{
						Header: nil,
					},
				},
			},
			expected: ratelimit.CategoryAll,
		},
		{
			name: "mixed items with nil",
			envelope: &protocol.Envelope{
				Header: &protocol.EnvelopeHeader{},
				Items: []*protocol.EnvelopeItem{
					nil,
					{
						Header: nil,
					},
					{
						Header: &protocol.EnvelopeItemHeader{
							Type: protocol.EnvelopeItemTypeEvent,
						},
					},
				},
			},
			expected: ratelimit.CategoryError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := categoryFromEnvelope(tt.envelope)
			if result != tt.expected {
				t.Errorf("categoryFromEnvelope() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestAsyncTransport_SendEnvelope(t *testing.T) {
	t.Run("empty dsn", func(t *testing.T) {
		transport := NewAsyncTransport(TransportOptions{})
		transport.Start()
		defer transport.Close()

		envelope := &protocol.Envelope{
			Header: &protocol.EnvelopeHeader{},
			Items:  []*protocol.EnvelopeItem{},
		}

		err := transport.SendEnvelope(envelope)
		if err == nil {
			t.Error("expected error for unconfigured transport")
		}
		if err.Error() != "transport not configured" {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("closed transport", func(t *testing.T) {
		transport := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
		transport.Start()
		transport.Close()

		envelope := &protocol.Envelope{
			Header: &protocol.EnvelopeHeader{},
			Items:  []*protocol.EnvelopeItem{},
		}

		err := transport.SendEnvelope(envelope)
		if !errors.Is(err, ErrTransportClosed) {
			t.Errorf("expected ErrTransportClosed, got %v", err)
		}
	})

	t.Run("queue full backpressure", func(t *testing.T) {
		queueSize := 3
		transport := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
		transport.Start()
		// simulate backpressure
		transport.queue = make(chan *protocol.Envelope, queueSize)
		defer transport.Close()

		envelope := &protocol.Envelope{
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
						Type: protocol.EnvelopeItemTypeEvent,
					},
					Payload: []byte(`{"message": "test"}`),
				},
			},
		}

		for i := 0; i < queueSize; i++ {
			err := transport.SendEnvelope(envelope)
			if err != nil {
				t.Errorf("envelope %d should succeed: %v", i, err)
			}
		}
		if err := transport.SendEnvelope(envelope); !errors.Is(err, ErrTransportQueueFull) {
			t.Errorf("envelope 3 should fail with err: %v", ErrTransportQueueFull)
		}
	})

	t.Run("rate limited envelope", func(t *testing.T) {
		transport := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
		transport.Start()
		defer transport.Close()

		transport.limits[ratelimit.CategoryError] = ratelimit.Deadline(time.Now().Add(time.Hour))

		envelope := &protocol.Envelope{
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
						Type: protocol.EnvelopeItemTypeEvent,
					},
					Payload: []byte(`{"message": "test"}`),
				},
			},
		}

		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("rate limited envelope should return nil error, got %v", err)
		}
	})
}

func TestAsyncTransport_Workers(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewAsyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))
	transport.Start()
	defer transport.Close()

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	for i := 0; i < 5; i++ {
		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("failed to send envelope %d: %v", i, err)
		}
	}

	// Use flush to wait for envelopes to be processed instead of sleep
	if !transport.Flush(testutils.FlushTimeout()) {
		t.Fatal("Flush timed out")
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != 5 {
		t.Errorf("expected 5 requests, got %d", finalCount)
	}

	if sentCount := atomic.LoadInt64(&transport.sentCount); sentCount != 5 {
		t.Errorf("expected sentCount to be 5, got %d", sentCount)
	}
}

func TestAsyncTransport_Flush(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		t.Logf("Received request %d", requestCount)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewAsyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))
	transport.Start()
	defer transport.Close()

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	// Send envelope
	err := transport.SendEnvelope(envelope)
	if err != nil {
		t.Errorf("failed to send envelope: %v", err)
	}

	success := transport.Flush(testutils.FlushTimeout())
	if !success {
		t.Error("flush should succeed")
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	if finalCount != 1 {
		t.Errorf("expected 1 request after flush, got %d", finalCount)
	}
}

// dummy test for coverage.
func TestSyncTransport_Flush(t *testing.T) {
	transport := NewSyncTransport(TransportOptions{})
	if !transport.Flush(testutils.FlushTimeout()) {
		t.Error("expected sync transport to flush correctly")
	}
	if !transport.FlushWithContext(context.Background()) {
		t.Error("expected sync transport to flush correctly")
	}
	transport.Close()
}

func TestAsyncTransport_ErrorHandling(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	transport := NewAsyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))
	transport.Start()

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	err := transport.SendEnvelope(envelope)
	if err != nil {
		t.Errorf("failed to send envelope: %v", err)
	}

	transport.Flush(testutils.FlushTimeout())
	transport.Close()

	mu.Lock()
	finalRequestCount := requestCount
	mu.Unlock()

	if finalRequestCount == 0 {
		t.Error("expected at least one HTTP request")
	}
}

func TestSyncTransport_SendEnvelope(t *testing.T) {
	t.Run("unconfigured transport", func(t *testing.T) {
		transport := NewSyncTransport(TransportOptions{})

		envelope := &protocol.Envelope{
			Header: &protocol.EnvelopeHeader{},
			Items:  []*protocol.EnvelopeItem{},
		}

		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("unconfigured transport should return nil, got %v", err)
		}
	})

	t.Run("successful send", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewSyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))

		envelope := &protocol.Envelope{
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
						Type: protocol.EnvelopeItemTypeEvent,
					},
					Payload: []byte(`{"message": "test"}`),
				},
			},
		}

		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("failed to send envelope: %v", err)
		}
	})

	t.Run("rate limited envelope", func(t *testing.T) {
		transport := NewSyncTransport(testTransportOptions("https://key@sentry.io/123"))

		transport.limits[ratelimit.CategoryError] = ratelimit.Deadline(time.Now().Add(time.Hour))

		envelope := &protocol.Envelope{
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
						Type: protocol.EnvelopeItemTypeEvent,
					},
					Payload: []byte(`{"message": "test"}`),
				},
			},
		}

		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("rate limited envelope should return nil error, got %v", err)
		}
	})
}

func TestTransportDefaults(t *testing.T) {
	t.Run("async transport defaults", func(t *testing.T) {
		transport := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
		transport.Start()
		defer transport.Close()

		if transport.QueueSize != defaultQueueSize {
			t.Errorf("QueueSize = %d, want %d", transport.QueueSize, defaultQueueSize)
		}
		if transport.Timeout != defaultTimeout {
			t.Errorf("Timeout = %v, want %v", transport.Timeout, defaultTimeout)
		}
	})

	t.Run("sync transport defaults", func(t *testing.T) {
		transport := NewSyncTransport(testTransportOptions("https://key@sentry.io/123"))

		if transport.Timeout != defaultTimeout {
			t.Errorf("Timeout = %v, want %v", transport.Timeout, defaultTimeout)
		}
	})
}

func TestAsyncTransport_CloseMultipleTimes(t *testing.T) {
	transport := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
	transport.Start()

	transport.Close()
	transport.Close()
	transport.Close()

	select {
	case <-transport.done:
	default:
		t.Error("transport should be closed")
	}

	var wg sync.WaitGroup
	transport2 := NewAsyncTransport(testTransportOptions("https://key@sentry.io/123"))
	transport2.Start()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			transport2.Close()
		}()
	}
	wg.Wait()

	select {
	case <-transport2.done:
	default:
		t.Error("transport2 should be closed")
	}
}

func TestSyncTransport_SendEvent(_ *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewSyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	event := &mockEnvelopeConvertible{envelope: envelope}
	transport.SendEvent(event)
}

func TestAsyncTransport_SendEvent(_ *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := NewAsyncTransport(testTransportOptions("http://key@" + server.URL[7:] + "/123"))
	transport.Start()
	defer transport.Close()

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	event := &mockEnvelopeConvertible{envelope: envelope}
	transport.SendEvent(event)

	transport.Flush(testutils.FlushTimeout())
}

// httptraceRoundTripper implements http.RoundTripper by wrapping
// http.DefaultTransport and keeps track of whether TCP connections have been
// reused for every request.
//
// For simplicity, httptraceRoundTripper is not safe for concurrent use.
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

func testKeepAlive(t *testing.T, isAsync bool) {
	// largeResponse controls whether the test server should simulate an
	// unexpectedly large response from Relay
	largeResponse := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulates a response from Relay
		fmt.Fprintln(w, `{"id":"ec71d87189164e79ab1e61030c183af0"}`)
		if largeResponse {
			fmt.Fprintln(w, strings.Repeat(" ", maxDrainResponseBytes))
		}
	}))
	defer srv.Close()

	dsn := "http://key@" + srv.URL[7:] + "/123"

	rt := &httptraceRoundTripper{}

	var transport interface {
		SendEnvelope(*protocol.Envelope) error
		Flush(time.Duration) bool
		Close()
	}

	if isAsync {
		asyncTransport := NewAsyncTransport(TransportOptions{
			Dsn:           dsn,
			HTTPTransport: rt,
		})
		if asyncTransport == nil {
			t.Fatal("Failed to create AsyncTransport")
		}
		asyncTransport.Start()
		defer func() {
			if asyncTransport != nil {
				asyncTransport.Close()
			}
		}()
		transport = asyncTransport
	} else {
		syncTransport := NewSyncTransport(TransportOptions{
			Dsn:           dsn,
			HTTPTransport: rt,
		})
		if syncTransport == nil {
			t.Fatal("Failed to create SyncTransport")
		}
		transport = syncTransport
	}

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	reqCount := 0
	checkLastConnReuse := func(reused bool) {
		t.Helper()
		reqCount++
		if transport == nil {
			t.Fatal("Transport is nil")
		}
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}
		if len(rt.reusedConn) != reqCount {
			t.Fatalf("unexpected number of requests: got %d, want %d", len(rt.reusedConn), reqCount)
		}
		if rt.reusedConn[reqCount-1] != reused {
			if reused {
				t.Fatal("TCP connection not reused")
			}
			t.Fatal("unexpected TCP connection reuse")
		}
	}

	// First event creates a new TCP connection
	if transport != nil {
		_ = transport.SendEnvelope(envelope)
		checkLastConnReuse(false)

		// Next events reuse the TCP connection
		for i := 0; i < 3; i++ {
			_ = transport.SendEnvelope(envelope)
			checkLastConnReuse(true)
		}

		// If server responses are too large, the SDK should close the
		// connection instead of consuming an arbitrarily large number of bytes
		largeResponse = true

		// Next event, first one to get a large response, reuses the connection
		_ = transport.SendEnvelope(envelope)
		checkLastConnReuse(true)

		// All future events create a new TCP connection
		for i := 0; i < 3; i++ {
			_ = transport.SendEnvelope(envelope)
			checkLastConnReuse(false)
		}
	} else {
		t.Fatal("Transport is nil")
	}
}

func TestKeepAlive(t *testing.T) {
	t.Run("AsyncTransport", func(t *testing.T) {
		testKeepAlive(t, true)
	})
	t.Run("SyncTransport", func(t *testing.T) {
		testKeepAlive(t, false)
	})
}

func testRateLimiting(t *testing.T, isAsync bool) {
	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "error"}`),
			},
		},
	}

	var requestCount int64

	// Test server that simulates rate limiting responses
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count := atomic.AddInt64(&requestCount, 1)
		if count == 1 {
			// First request gets rate limited
			w.Header().Add("Retry-After", "1")
			w.Header().Add("X-Sentry-Rate-Limits", "1:error")
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			// Subsequent requests should be blocked by rate limiting
			w.WriteHeader(http.StatusOK)
		}
		fmt.Fprint(w, `{"id":"636205708f6846c8821e6576a9d05921"}`)
	}))
	defer srv.Close()

	dsn := "http://key@" + srv.URL[7:] + "/123"

	var transport interface {
		SendEnvelope(*protocol.Envelope) error
		Flush(time.Duration) bool
		Close()
	}

	if isAsync {
		asyncTransport := NewAsyncTransport(TransportOptions{Dsn: dsn})
		if asyncTransport == nil {
			t.Fatal("Failed to create AsyncTransport")
		}
		asyncTransport.Start()
		defer func() {
			if asyncTransport != nil {
				asyncTransport.Close()
			}
		}()
		transport = asyncTransport
	} else {
		syncTransport := NewSyncTransport(TransportOptions{Dsn: dsn})
		if syncTransport == nil {
			t.Fatal("Failed to create SyncTransport")
		}
		transport = syncTransport
	}

	if transport == nil {
		t.Fatal("Transport is nil")
	}

	// Send first envelope - this should reach server and get rate limited
	_ = transport.SendEnvelope(envelope)

	// Send more envelopes - these should be blocked by rate limiting
	for i := 0; i < 3; i++ {
		_ = transport.SendEnvelope(envelope)
	}

	if !transport.Flush(testutils.FlushTimeout()) {
		t.Fatal("Flush timed out")
	}

	// At most 1-2 requests should reach the server before rate limiting kicks in
	finalCount := atomic.LoadInt64(&requestCount)
	if finalCount > 2 {
		t.Errorf("expected at most 2 requests to reach server, got %d", finalCount)
	}
	if finalCount < 1 {
		t.Errorf("expected at least 1 request to reach server, got %d", finalCount)
	}
}

func TestRateLimiting(t *testing.T) {
	t.Run("AsyncTransport", func(t *testing.T) {
		testRateLimiting(t, true)
	})
	t.Run("SyncTransport", func(t *testing.T) {
		testRateLimiting(t, false)
	})
}

func TestAsyncTransport_ErrorHandling_Simple(t *testing.T) {
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		// Always fail to test error handling
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	transport := NewAsyncTransport(TransportOptions{
		Dsn: "http://key@" + server.URL[7:] + "/123",
	})
	if transport == nil {
		t.Fatal("Failed to create AsyncTransport")
	}
	transport.Start()
	defer func() {
		if transport != nil {
			transport.Close()
		}
	}()

	envelope := &protocol.Envelope{
		Header: &protocol.EnvelopeHeader{
			EventID: "error-test-id",
			Sdk: &protocol.SdkInfo{
				Name:    "test",
				Version: "1.0.0",
			},
		},
		Items: []*protocol.EnvelopeItem{
			{
				Header: &protocol.EnvelopeItemHeader{
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "error test"}`),
			},
		},
	}

	if transport != nil {
		err := transport.SendEnvelope(envelope)
		if err != nil {
			t.Errorf("failed to send envelope: %v", err)
		}

		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}
	} else {
		t.Fatal("Transport is nil")
	}

	mu.Lock()
	finalCount := requestCount
	mu.Unlock()

	// Should make exactly one request (no retries)
	if finalCount != 1 {
		t.Errorf("expected exactly 1 request (no retries), got %d", finalCount)
	}

	// Should have 0 successful sends and 1 error
	sentCount := atomic.LoadInt64(&transport.sentCount)
	errorCount := atomic.LoadInt64(&transport.errorCount)

	if sentCount != 0 {
		t.Errorf("expected 0 successful sends, got %d", sentCount)
	}
	if errorCount != 1 {
		t.Errorf("expected 1 error, got %d", errorCount)
	}
}

func TestAsyncTransportDoesntLeakGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	transport := NewAsyncTransport(TransportOptions{
		Dsn: "https://test@foobar/1",
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, fmt.Errorf("mock transport - no real connections")
				},
			},
		},
	})

	if transport == nil {
		t.Fatal("Failed to create AsyncTransport")
	}

	transport.Start()

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	if transport != nil {
		_ = transport.SendEnvelope(envelope)
		transport.Flush(testutils.FlushTimeout())
		transport.Close()
	}
}

func TestConcurrentAccess(t *testing.T) {
	t.Run("AsyncTransport", func(t *testing.T) {
		testConcurrentAccess(t, true)
	})
	t.Run("SyncTransport", func(t *testing.T) {
		testConcurrentAccess(t, false)
	})
}

func testConcurrentAccess(t *testing.T, isAsync bool) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate rate limiting on some requests
		if atomic.LoadInt64(&requestCounter)%3 == 0 {
			w.Header().Add("X-Sentry-Rate-Limits", "10:error")
			w.WriteHeader(http.StatusTooManyRequests)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		atomic.AddInt64(&requestCounter, 1)
	}))
	defer server.Close()

	var transport interface {
		SendEnvelope(*protocol.Envelope) error
		Flush(time.Duration) bool
		Close()
	}

	if isAsync {
		asyncTransport := NewAsyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		if asyncTransport == nil {
			t.Fatal("Failed to create AsyncTransport")
		}
		asyncTransport.Start()
		defer func() {
			if asyncTransport != nil {
				asyncTransport.Close()
			}
		}()
		transport = asyncTransport
	} else {
		syncTransport := NewSyncTransport(TransportOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		if syncTransport == nil {
			t.Fatal("Failed to create SyncTransport")
		}
		transport = syncTransport
	}

	envelope := &protocol.Envelope{
		Header: &protocol.EnvelopeHeader{
			EventID: "concurrent-test-id",
			Sdk: &protocol.SdkInfo{
				Name:    "test",
				Version: "1.0.0",
			},
		},
		Items: []*protocol.EnvelopeItem{
			{
				Header: &protocol.EnvelopeItemHeader{
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "concurrent test"}`),
			},
		},
	}

	if transport == nil {
		t.Fatal("Transport is nil")
	}

	// Send envelopes concurrently to test thread-safety
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				if transport != nil {
					_ = transport.SendEnvelope(envelope)
				}
			}
		}()
	}
	wg.Wait()

	if transport != nil {
		transport.Flush(testutils.FlushTimeout())
	}
}

var requestCounter int64

func TestIsRateLimited(t *testing.T) {
	t.Run("AsyncTransport", func(t *testing.T) {
		testIsRateLimited(t, true)
	})
	t.Run("SyncTransport", func(t *testing.T) {
		testIsRateLimited(t, false)
	})
}

func testIsRateLimited(t *testing.T, isAsync bool) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Retry-After", "60")
		w.Header().Add("X-Sentry-Rate-Limits", "60:error,120:transaction")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"id":"test"}`)
	}))
	defer srv.Close()

	dsn := "http://key@" + srv.URL[7:] + "/123"

	var transport interface {
		SendEnvelope(*protocol.Envelope) error
		IsRateLimited(ratelimit.Category) bool
		Flush(time.Duration) bool
		Close()
	}

	if isAsync {
		asyncTransport := NewAsyncTransport(TransportOptions{Dsn: dsn})
		if asyncTransport == nil {
			t.Fatal("Failed to create AsyncTransport")
		}
		asyncTransport.Start()
		defer func() {
			if asyncTransport != nil {
				asyncTransport.Close()
			}
		}()
		transport = asyncTransport
	} else {
		syncTransport := NewSyncTransport(TransportOptions{Dsn: dsn})
		if syncTransport == nil {
			t.Fatal("Failed to create SyncTransport")
		}
		transport = syncTransport
	}

	if transport == nil {
		t.Fatal("Transport is nil")
	}

	if transport.IsRateLimited(ratelimit.CategoryError) {
		t.Error("CategoryError should not be rate limited initially")
	}
	if transport.IsRateLimited(ratelimit.CategoryTransaction) {
		t.Error("CategoryTransaction should not be rate limited initially")
	}
	if transport.IsRateLimited(ratelimit.CategoryAll) {
		t.Error("CategoryAll should not be rate limited initially")
	}

	envelope := &protocol.Envelope{
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
					Type: protocol.EnvelopeItemTypeEvent,
				},
				Payload: []byte(`{"message": "test"}`),
			},
		},
	}

	_ = transport.SendEnvelope(envelope)

	if !transport.Flush(testutils.FlushTimeout()) {
		t.Fatal("Flush timed out")
	}

	// After receiving rate limit response, categories should be rate limited
	if !transport.IsRateLimited(ratelimit.CategoryError) {
		t.Error("CategoryError should be rate limited after server response")
	}
	if !transport.IsRateLimited(ratelimit.CategoryTransaction) {
		t.Error("CategoryTransaction should be rate limited after server response")
	}

	// CategoryAll should not be rate limited since we only got specific category limits
	if transport.IsRateLimited(ratelimit.CategoryAll) {
		t.Error("CategoryAll should not be rate limited with specific category limits")
	}

	// Other categories should not be rate limited
	if transport.IsRateLimited(ratelimit.CategoryMonitor) {
		t.Error("CategoryMonitor should not be rate limited")
	}
	if transport.IsRateLimited(ratelimit.CategoryLog) {
		t.Error("CategoryLog should not be rate limited")
	}
}

func TestTransportConfiguration_ProxyAndTLS(t *testing.T) {
	t.Run("HTTPProxy configuration", func(t *testing.T) {
		options := TransportOptions{
			Dsn:       "https://key@sentry.io/123",
			HTTPProxy: "http://proxy:8080",
		}

		transport := NewAsyncTransport(options)
		defer transport.Close()

		if transport.client == nil {
			t.Error("Expected HTTP client to be configured")
		}

		if httpTransport, ok := transport.transport.(*http.Transport); ok {
			if httpTransport.Proxy == nil {
				t.Error("Expected proxy function to be set")
			}

			req, _ := http.NewRequest("GET", "https://example.com", nil)
			proxyURL, err := httpTransport.Proxy(req)
			if err != nil {
				t.Errorf("Proxy function returned error: %v", err)
			}
			if proxyURL == nil {
				t.Error("Expected proxy URL to be set")
			} else if proxyURL.String() != "http://proxy:8080" {
				t.Errorf("Expected proxy URL 'http://proxy:8080', got '%s'", proxyURL.String())
			}
		} else {
			t.Error("Expected transport to be *http.Transport")
		}
	})

	t.Run("HTTPSProxy configuration", func(t *testing.T) {
		options := TransportOptions{
			Dsn:        "https://key@sentry.io/123",
			HTTPSProxy: "https://secure-proxy:8443",
		}

		transport := NewAsyncTransport(options)
		defer transport.Close()

		if transport.client == nil {
			t.Error("Expected HTTP client to be configured")
		}

		if httpTransport, ok := transport.transport.(*http.Transport); ok {
			if httpTransport.Proxy == nil {
				t.Error("Expected proxy function to be set")
			}

			req, _ := http.NewRequest("GET", "https://example.com", nil)
			proxyURL, err := httpTransport.Proxy(req)
			if err != nil {
				t.Errorf("Proxy function returned error: %v", err)
			}
			if proxyURL == nil {
				t.Error("Expected proxy URL to be set")
			} else if proxyURL.String() != "https://secure-proxy:8443" {
				t.Errorf("Expected proxy URL 'https://secure-proxy:8443', got '%s'", proxyURL.String())
			}
		} else {
			t.Error("Expected transport to be *http.Transport")
		}
	})

	t.Run("Custom HTTPTransport overrides proxy config", func(t *testing.T) {
		customTransport := &http.Transport{}

		options := TransportOptions{
			Dsn:           "https://key@sentry.io/123",
			HTTPTransport: customTransport,
			HTTPProxy:     "http://proxy:8080",
		}

		transport := NewAsyncTransport(options)
		defer transport.Close()

		if transport.client == nil {
			t.Error("Expected HTTP client to be configured")
		}

		if transport.transport != customTransport {
			t.Error("Expected custom HTTPTransport to be used, ignoring proxy config")
		}

		if transport.transport.(*http.Transport).Proxy != nil {
			t.Error("Custom transport should not have proxy config from options")
		}
	})

	t.Run("CaCerts configuration", func(t *testing.T) {
		certPool := x509.NewCertPool()

		options := TransportOptions{
			Dsn:     "https://key@sentry.io/123",
			CaCerts: certPool,
		}

		transport := NewSyncTransport(options)

		if transport.client == nil {
			t.Error("Expected HTTP client to be configured")
		}

		if httpTransport, ok := transport.transport.(*http.Transport); ok {
			if httpTransport.TLSClientConfig == nil {
				t.Error("Expected TLS client config to be set")
			} else if httpTransport.TLSClientConfig.RootCAs != certPool {
				t.Error("Expected custom certificate pool to be used")
			}
		} else {
			t.Error("Expected transport to be *http.Transport")
		}
	})
}
