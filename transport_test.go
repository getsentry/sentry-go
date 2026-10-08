package sentry

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"go.uber.org/goleak"
)

func takeOutcomes(recorder *report.Aggregator) map[report.OutcomeKey]int64 {
	outcomes := make(map[report.OutcomeKey]int64)
	if clientReport := recorder.TakeReport(); clientReport != nil {
		for _, event := range clientReport.DiscardedEvents {
			outcomes[report.OutcomeKey{Reason: event.Reason, Category: event.Category}] += event.Quantity
		}
	}
	return outcomes
}

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
		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{})

		defer transport.Close()
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Error("disabled transport should flush without blocking")
		}

		err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("invalid DSN should return nil, got %v", err)
		}
	})

	t.Run("closed transport", func(t *testing.T) {
		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{Dsn: "https://key@sentry.io/123"})
		transport.Close()

		err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !errors.Is(err, ErrTransportClosed) {
			t.Errorf("expected ErrTransportClosed, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name     string
			envelope *protocol.Envelope
			wantErr  error
		}{
			{"event", testEnvelope(protocol.EnvelopeItemTypeEvent), nil},
			{"transaction", testEnvelope(protocol.EnvelopeItemTypeTransaction), nil},
			{"check-in", testEnvelope(protocol.EnvelopeItemTypeCheckIn), nil},
			{"log", testEnvelope(protocol.EnvelopeItemTypeLog), nil},
			{"attachment", testEnvelope(protocol.EnvelopeItemTypeAttachment), nil},
			{"nil header", &protocol.Envelope{Items: testEnvelope(protocol.EnvelopeItemTypeEvent).Items}, ErrInvalidEnvelope},
		}

		var count int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&count, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		for _, tt := range tests {
			if err := transport.SendEnvelope(context.Background(), tt.envelope); !errors.Is(err, tt.wantErr) {
				t.Errorf("send %s returned %v, want %v", tt.name, err, tt.wantErr)
			}
		}

		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		expectedCount := int64(len(tests) - 1) // one invalid envelope
		if sent := atomic.LoadInt64(&count); sent != expectedCount {
			t.Errorf("expected %d sent, got %d", expectedCount, sent)
		}
	})

	t.Run("server error", func(t *testing.T) {
		var requestCount int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&requestCount, 1)
			status := http.StatusInternalServerError
			w.WriteHeader(status)
		}))
		defer server.Close()

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		if err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
			t.Fatalf("failed to send envelope: %v", err)
		}

		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		if sent := atomic.LoadInt64(&requestCount); sent != 1 {
			t.Errorf("expected 1 request, got %d", sent)
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

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}

		if !transport.isRateLimited(ratelimit.CategoryError) {
			t.Error("error category should be rate limited")
		}
		if !transport.isRateLimited(ratelimit.CategoryTransaction) {
			t.Error("transaction category should be rate limited")
		}
		if transport.isRateLimited(ratelimit.CategoryMonitor) {
			t.Error("monitor category should not be rate limited")
		}

		for i := 0; i < 2; i++ {
			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		}
		if !transport.Flush(testutils.FlushTimeout()) {
			t.Fatal("Flush timed out")
		}
	})

	t.Run("queue overflow", func(t *testing.T) {
		blockChan := make(chan struct{})
		requestReceived := make(chan struct{}, 1)
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
		recorder := report.NewAggregator()
		transport := &AsyncTransport{
			httpSender: &httpSender{
				limits:    make(ratelimit.Map),
				dsn:       dsn,
				transport: &http.Transport{},
				client:    &http.Client{Timeout: defaultTimeout},
				recorder:  recorder,
			},
			QueueSize: 2,
			Timeout:   defaultTimeout,
			done:      make(chan struct{}),
		}
		// manually set the queue size to simulate overflow
		transport.queue = make(chan *protocol.Envelope, transport.QueueSize)
		transport.flushRequest = make(chan flushRequest)
		transport.start()
		defer func() {
			close(blockChan)
			transport.Close()
		}()

		if err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
			t.Fatalf("first send should succeed: %v", err)
		}

		<-requestReceived

		for i := 0; i < transport.QueueSize; i++ {
			if err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
				t.Errorf("send %d should succeed: %v", i, err)
			}
		}

		err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		if !errors.Is(err, ErrTransportQueueFull) {
			t.Errorf("expected ErrTransportQueueFull, got %v", err)
		}
		if outcomes := takeOutcomes(recorder); len(outcomes) != 0 {
			t.Errorf("rejected envelope should not be recorded by the transport, got %v", outcomes)
		}
	})

	t.Run("FlushMultipleTimes", func(t *testing.T) {
		var count int64
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			atomic.AddInt64(&count, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		if err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
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

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))

		ctx := context.Background()
		if !transport.FlushWithContext(ctx) {
			t.Error("FlushWithContext should succeed")
		}
	})

	t.Run("timeout", func(t *testing.T) {
		blockChan := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			<-blockChan
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer func() {
			close(blockChan)
			transport.Close()
		}()

		_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		if transport.FlushWithContext(ctx) {
			t.Error("FlushWithContext should timeout")
		}
	})

	t.Run("closed transport", func(t *testing.T) {
		transport := NewAsyncTransport()
		transport.Configure(ClientOptions{Dsn: "https://key@sentry.io/123"})
		transport.Close()

		if transport.FlushWithContext(context.Background()) {
			t.Error("FlushWithContext should fail for a closed transport")
		}
	})
}

func TestAsyncTransport_Close(t *testing.T) {
	transport := NewAsyncTransport()
	transport.Configure(ClientOptions{
		Dsn: "https://key@sentry.io/123",
	})

	transport.Close()
	transport.Close()
	transport.Close()

	select {
	case <-transport.done:
	default:
		t.Error("transport should be closed")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPTransportDeadlines(t *testing.T) {
	// The supplied client has no timeout, so only the transport bounds requests.
	options := ClientOptions{
		Dsn: "https://key@sentry.io/123",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			<-r.Context().Done()
			return nil, r.Context().Err()
		})},
	}

	t.Run("sync request timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			transport := NewSyncTransport()
			transport.Configure(options)
			transport.Timeout = time.Second
			start := time.Now()
			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
			if elapsed := time.Since(start); elapsed != time.Second {
				t.Errorf("request took %v, want %v", elapsed, time.Second)
			}
		})
	})

	t.Run("async request timeout", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			transport := NewAsyncTransport()
			transport.Configure(options)
			defer transport.Close()
			transport.Timeout = time.Second
			start := time.Now()
			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
			if !transport.Flush(time.Minute) {
				t.Error("Flush should succeed after the request times out")
			}
			if elapsed := time.Since(start); elapsed != time.Second {
				t.Errorf("request took %v, want %v", elapsed, time.Second)
			}
		})
	})

	t.Run("async flush deadline and close", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			transport := NewAsyncTransport()
			transport.Configure(options)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if transport.FlushWithContext(ctx) {
				t.Error("FlushWithContext should fail for a canceled context")
			}
			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
			if transport.Flush(time.Second) {
				t.Error("Flush should time out while a request is in flight")
			}
			start := time.Now()
			transport.Close()
			if elapsed := time.Since(start); elapsed != 0 {
				t.Errorf("Close waited %v for the in-flight request", elapsed)
			}
		})
	})
}

func TestSyncTransport_SendEnvelope(t *testing.T) {
	t.Run("invalid DSN", func(t *testing.T) {
		transport := NewSyncTransport()
		transport.Configure(ClientOptions{})
		err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		if err != nil {
			t.Errorf("invalid DSN should return nil, got %v", err)
		}
	})

	t.Run("success", func(t *testing.T) {
		tests := []struct {
			name     string
			envelope *protocol.Envelope
			wantErr  error
		}{
			{"event", testEnvelope(protocol.EnvelopeItemTypeEvent), nil},
			{"transaction", testEnvelope(protocol.EnvelopeItemTypeTransaction), nil},
			{"check-in", testEnvelope(protocol.EnvelopeItemTypeCheckIn), nil},
			{"log", testEnvelope(protocol.EnvelopeItemTypeLog), nil},
			{"attachment", testEnvelope(protocol.EnvelopeItemTypeAttachment), nil},
			{"nil envelope", nil, ErrInvalidEnvelope},
			{"nil header", &protocol.Envelope{Items: testEnvelope(protocol.EnvelopeItemTypeEvent).Items}, ErrInvalidEnvelope},
			{"no items", &protocol.Envelope{Header: &protocol.EnvelopeHeader{}}, ErrInvalidEnvelope},
			{"nil item", &protocol.Envelope{Header: &protocol.EnvelopeHeader{}, Items: []*protocol.EnvelopeItem{nil}}, ErrInvalidEnvelope},
			{"nil item header", &protocol.Envelope{Header: &protocol.EnvelopeHeader{}, Items: []*protocol.EnvelopeItem{{}}}, ErrInvalidEnvelope},
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		transport := NewSyncTransport()
		transport.Configure(ClientOptions{
			Dsn: "http://key@" + server.URL[7:] + "/123",
		})
		defer transport.Close()

		for _, tt := range tests {
			if err := transport.SendEnvelope(context.Background(), tt.envelope); !errors.Is(err, tt.wantErr) {
				t.Errorf("send %s returned %v, want %v", tt.name, err, tt.wantErr)
			}
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.Header().Add("X-Sentry-Rate-Limits", "60:error,60:transaction,60:trace_metric")
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		recorder := report.NewAggregator()
		transport := NewSyncTransport()
		transport.Configure(ClientOptions{
			Dsn:      "http://key@" + server.URL[7:] + "/123",
			recorder: recorder,
		})

		_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
		_ = takeOutcomes(recorder)

		pending, _ := (&report.ClientReport{DiscardedEvents: []report.DiscardedEvent{
			{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 2},
		}}).ToEnvelopeItem()
		backoff := func(category ratelimit.Category) report.OutcomeKey {
			return report.OutcomeKey{Reason: report.ReasonRateLimitBackoff, Category: category}
		}
		tests := []struct {
			name     string
			envelope *protocol.Envelope
			want     map[report.OutcomeKey]int64 // nil when the envelope is sent
		}{
			{"event", testEnvelope(protocol.EnvelopeItemTypeEvent), map[report.OutcomeKey]int64{backoff(ratelimit.CategoryError): 1}},
			{"transaction", testEnvelope(protocol.EnvelopeItemTypeTransaction), map[report.OutcomeKey]int64{backoff(ratelimit.CategoryTransaction): 1}},
			{"trace metric", protocol.NewEnvelope(&protocol.EnvelopeHeader{}, protocol.NewTraceMetricItem(3, []byte(`{"items":[]}`))),
				map[report.OutcomeKey]int64{backoff(ratelimit.CategoryTraceMetric): 3}},
			{"client report before event", protocol.NewEnvelope(&protocol.EnvelopeHeader{}, pending, testEnvelope(protocol.EnvelopeItemTypeEvent).Items[0]),
				map[report.OutcomeKey]int64{backoff(ratelimit.CategoryError): 1, {Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError}: 2}},
			{"check-in", testEnvelope(protocol.EnvelopeItemTypeCheckIn), nil},
		}
		for _, tt := range tests {
			sent := requests.Load()
			if err := transport.SendEnvelope(context.Background(), tt.envelope); err != nil {
				t.Errorf("%s: rate limited envelope should return nil, got %v", tt.name, err)
			}
			if gotSent := requests.Load() > sent; gotSent != (tt.want == nil) {
				t.Errorf("%s: sent = %v, want %v", tt.name, gotSent, tt.want == nil)
			}
			if outcomes := takeOutcomes(recorder); tt.want != nil && fmt.Sprint(outcomes) != fmt.Sprint(tt.want) {
				t.Errorf("%s: got outcomes %v, want %v", tt.name, outcomes, tt.want)
			}
		}
	})

	t.Run("delivery errors", func(t *testing.T) {
		tests := []struct {
			name      string
			roundTrip roundTripperFunc
			reason    report.DiscardReason
		}{
			{"server error", func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(strings.NewReader("internal error"))}, nil
			}, report.ReasonSendError},
			{"network error", func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			}, report.ReasonNetworkError},
		}
		for _, tt := range tests {
			recorder := report.NewAggregator()
			transport := NewSyncTransport()
			transport.Configure(ClientOptions{
				Dsn: "https://key@sentry.io/123", recorder: recorder, HTTPTransport: tt.roundTrip,
			})

			// The transport records losses of accepted envelopes itself.
			if err := transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent)); err != nil {
				t.Errorf("%s: should not return error, got %v", tt.name, err)
			}
			want := map[report.OutcomeKey]int64{{Reason: tt.reason, Category: ratelimit.CategoryError}: 1}
			if outcomes := takeOutcomes(recorder); fmt.Sprint(outcomes) != fmt.Sprint(want) {
				t.Errorf("%s: got outcomes %v, want %v", tt.name, outcomes, want)
			}
		}
	})
}

func TestSyncTransport_Flush(t *testing.T) {
	transport := NewSyncTransport()
	transport.Configure(ClientOptions{})

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

	// After go 1.27, the runtime reuses http connections when the response body is less than 256 KiB automatically.
	// To test keepalive we need a response body larger than this limit.
	maxDrainResponseBytes := 256 << 11
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			largeResponse := false
			largeResponseBody := strings.Repeat(" ", maxDrainResponseBytes)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintln(w, `{"id":"ec71d87189164e79ab1e61030c183af0"}`)
				if largeResponse {
					fmt.Fprintln(w, largeResponseBody)
				}
			}))
			defer server.Close()

			rt := &httptraceRoundTripper{}
			dsn := "http://key@" + server.URL[7:] + "/123"

			var transport Transport

			if tt.async {
				asyncTransport := NewAsyncTransport()
				asyncTransport.Configure(ClientOptions{
					Dsn:           dsn,
					HTTPTransport: rt,
				})
				defer asyncTransport.Close()
				transport = asyncTransport
			} else {
				transport = NewSyncTransport()
				transport.Configure(ClientOptions{
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

			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
			checkReuse(false)

			for i := 0; i < 3; i++ {
				_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
				checkReuse(true)
			}

			largeResponse = true

			_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
			checkReuse(true)

			for i := 0; i < 3; i++ {
				_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
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

			var transport Transport

			if tt.async {
				asyncTransport := NewAsyncTransport()
				asyncTransport.Configure(ClientOptions{Dsn: dsn})
				defer asyncTransport.Close()
				transport = asyncTransport
			} else {
				transport = NewSyncTransport()
				transport.Configure(ClientOptions{Dsn: dsn})
			}

			var wg sync.WaitGroup
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 5; j++ {
						_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
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
		options  ClientOptions
		async    bool
		validate func(*testing.T, interface{})
	}{
		{
			name: "HTTPProxy",
			options: ClientOptions{
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
			options: ClientOptions{
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
			options: ClientOptions{
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
			options: ClientOptions{
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
			options: ClientOptions{
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
			options: ClientOptions{
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
				transport := NewAsyncTransport()
				transport.Configure(tt.options)
				defer transport.Close()
				tt.validate(t, transport)
			} else {
				transport := NewSyncTransport()
				transport.Configure(tt.options)
				tt.validate(t, transport)
			}
		})
	}
}

func TestAsyncTransportDoesntLeakGoroutines(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	transport := NewAsyncTransport()
	transport.Configure(ClientOptions{
		Dsn: "https://test@foobar/1",
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
					return nil, fmt.Errorf("mock transport")
				},
			},
		},
	})

	_ = transport.SendEnvelope(context.Background(), testEnvelope(protocol.EnvelopeItemTypeEvent))
	transport.Flush(testutils.FlushTimeout())
	transport.Close()
}
