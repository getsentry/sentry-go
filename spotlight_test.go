package sentry

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
)

func TestSpotlightTransport(t *testing.T) {
	// Mock Spotlight server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/stream" {
			t.Errorf("Expected /stream, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-sentry-envelope" {
			t.Errorf("Expected application/x-sentry-envelope, got %s", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != sdkIdentifier+"/"+SDKVersion {
			t.Errorf("Expected %s/%s, got %s", sdkIdentifier, SDKVersion, ua)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	event := NewEvent()
	event.Sdk.Name = "sentry-go"
	event.Sdk.Version = SDKVersion
	event.Message = "Test message"
	st.SendEvent(event)

	// The underlying transport is called synchronously by SendEvent, before
	// the Spotlight goroutine is spawned, so this assertion needs no sleep.
	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event, got %d", len(mock.Events()))
	}
	if mock.Events()[0].Message != "Test message" {
		t.Errorf("Expected 'Test message', got %s", mock.Events()[0].Message)
	}

	// Flush waits for the Spotlight goroutine, which is what actually POSTs
	// to the test server and exercises the header assertions above.
	if !st.Flush(time.Second) {
		t.Errorf("Expected Flush to succeed")
	}

	if mock.FlushCount() != 1 {
		t.Errorf("Expected underlying transport Flush called 1 time, got %d", mock.FlushCount())
	}
}

// TestSpotlightTransportWithNoopUnderlying verifies Spotlight still works when
// there's no real DSN configured (the underlying transport is noopTransport,
// e.g. Spotlight-only local development).
func TestSpotlightTransportWithNoopUnderlying(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	st := NewSpotlightTransport(noopTransport{})
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})
	defer st.Close()

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	if !st.Flush(time.Second) {
		t.Fatalf("Expected Flush to succeed")
	}
	if got := requestCount.Load(); got != 1 {
		t.Errorf("Expected 1 request to reach Spotlight, got %d", got)
	}
}

func TestSpotlightClientOptions(t *testing.T) {
	tests := []struct {
		name         string
		options      ClientOptions
		envVar       string
		wantErr      bool
		hasSpotlight bool
	}{
		{
			name: "Spotlight enabled with DSN",
			options: ClientOptions{
				Dsn:       "https://user@sentry.io/123",
				Spotlight: true,
			},
			hasSpotlight: true,
		},
		{
			name: "Spotlight enabled without DSN",
			options: ClientOptions{
				Spotlight: true,
			},
			hasSpotlight: true,
		},
		{
			name: "Spotlight disabled",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			hasSpotlight: false,
		},
		{
			name: "Spotlight with custom URL",
			options: ClientOptions{
				Spotlight:    true,
				SpotlightURL: "http://custom:9000/events",
			},
			hasSpotlight: true,
		},
		{
			// Regression test: SpotlightURL alone, with no Spotlight:true and
			// no env var, must still enable Spotlight per the spec's
			// two-attribute rule ("spotlightUrl implies spotlight:true").
			name: "SpotlightURL alone implies enabled",
			options: ClientOptions{
				SpotlightURL: "http://custom:9000/events",
			},
			hasSpotlight: true,
		},
		{
			name: "Spotlight enabled via env var",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			envVar:       "true",
			hasSpotlight: true,
		},
		{
			name: "Spotlight enabled via env var (numeric)",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			envVar:       "1",
			hasSpotlight: true,
		},
		{
			name: "Spotlight disabled via env var",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			envVar:       "false",
			hasSpotlight: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVar != "" {
				t.Setenv("SENTRY_SPOTLIGHT", tt.envVar)
			}

			client, err := NewClient(tt.options)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Spotlight is wired up via the default telemetry processor path
			// (see TestSpotlightUsesTelemetryProcessorByDefault) or, for
			// custom Transports/DisableTelemetryBuffer, via SpotlightTransport
			// (see TestSpotlightClientOptionsLegacyTransportPath). What this
			// test actually exercises is config/env-var resolution, so check
			// the resolved option directly.
			if client.options.Spotlight != tt.hasSpotlight {
				t.Errorf("Expected options.Spotlight = %v, got %v", tt.hasSpotlight, client.options.Spotlight)
			}
		})
	}
}

func TestSpotlightURLPrecedence(t *testing.T) {
	defaultURL := "http://localhost:8969/stream"

	tests := []struct {
		name        string
		options     ClientOptions
		envVar      string
		wantURL     string
		description string
	}{
		{
			name: "Default URL when spotlight=true, no URL, no env var",
			options: ClientOptions{
				Spotlight: true,
			},
			wantURL:     defaultURL,
			description: "Should use default URL",
		},
		{
			name: "Config URL takes precedence over env var URL",
			options: ClientOptions{
				Spotlight:    true,
				SpotlightURL: "http://config.url/stream",
			},
			envVar:      "http://env.url/stream",
			wantURL:     "http://config.url/stream",
			description: "Config URL should take precedence",
		},
		{
			name: "Env var URL used when spotlight=true, no URL, SENTRY_SPOTLIGHT=URL",
			options: ClientOptions{
				Spotlight: true,
			},
			envVar:      "http://env.url/stream",
			wantURL:     "http://env.url/stream",
			description: "Env var URL should be used",
		},
		{
			name: "Env var URL used when no config, SENTRY_SPOTLIGHT=URL",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			envVar:      "http://env.url/stream",
			wantURL:     "http://env.url/stream",
			description: "Env var URL should be used",
		},
		{
			name: "Default URL when SENTRY_SPOTLIGHT=true, no config",
			options: ClientOptions{
				Dsn: "https://user@sentry.io/123",
			},
			envVar:      "true",
			wantURL:     defaultURL,
			description: "Default URL should be used",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envVar != "" {
				t.Setenv("SENTRY_SPOTLIGHT", tt.envVar)
			} else {
				t.Setenv("SENTRY_SPOTLIGHT", "")
			}

			client, err := NewClient(tt.options)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			gotURL := client.options.SpotlightURL
			if gotURL == "" {
				gotURL = defaultSpotlightURL
			}
			if gotURL != tt.wantURL {
				t.Errorf("%s: Expected URL %s, got %s", tt.description, tt.wantURL, gotURL)
			}
		})
	}
}

func TestParseSpotlightEnvVar(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		wantEnabled bool
		wantURL     string
	}{
		// Truthy values
		{
			name:        "true",
			value:       "true",
			wantEnabled: true,
			wantURL:     "",
		},
		{
			name:        "t",
			value:       "t",
			wantEnabled: true,
			wantURL:     "",
		},
		{
			name:        "y",
			value:       "y",
			wantEnabled: true,
			wantURL:     "",
		},
		{
			name:        "yes",
			value:       "yes",
			wantEnabled: true,
			wantURL:     "",
		},
		{
			name:        "on",
			value:       "on",
			wantEnabled: true,
			wantURL:     "",
		},
		{
			name:        "1",
			value:       "1",
			wantEnabled: true,
			wantURL:     "",
		},
		// Falsy values
		{
			name:        "false",
			value:       "false",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "f",
			value:       "f",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "n",
			value:       "n",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "no",
			value:       "no",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "off",
			value:       "off",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "0",
			value:       "0",
			wantEnabled: false,
			wantURL:     "",
		},
		// URL values
		{
			name:        "custom URL",
			value:       "http://custom:9000/stream",
			wantEnabled: true,
			wantURL:     "http://custom:9000/stream",
		},
		{
			name:        "localhost URL",
			value:       "http://localhost:8969/stream",
			wantEnabled: true,
			wantURL:     "http://localhost:8969/stream",
		},
		// Edge cases
		{
			name:        "empty string",
			value:       "",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "whitespace only",
			value:       "   ",
			wantEnabled: false,
			wantURL:     "",
		},
		{
			name:        "case insensitive true",
			value:       "TRUE",
			wantEnabled: true,
			wantURL:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSpotlightEnvVar(tt.value)
			if result.enabled != tt.wantEnabled {
				t.Errorf("Expected enabled=%v, got %v", tt.wantEnabled, result.enabled)
			}
			if result.url != tt.wantURL {
				t.Errorf("Expected url=%q, got %q", tt.wantURL, result.url)
			}
		})
	}
}

func TestSpotlightSampleRateOverride(t *testing.T) {
	tests := []struct {
		name               string
		inputSampleRate    float64
		expectedSampleRate float64
	}{
		{
			name:               "Sample rate 0.5 overridden to 1.0",
			inputSampleRate:    0.5,
			expectedSampleRate: 1.0,
		},
		{
			name:               "Sample rate 0.0 overridden to 1.0",
			inputSampleRate:    0.0,
			expectedSampleRate: 1.0,
		},
		{
			name:               "Sample rate 1.0 unchanged",
			inputSampleRate:    1.0,
			expectedSampleRate: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				Spotlight:  true,
				SampleRate: tt.inputSampleRate,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			if client.options.SampleRate != tt.expectedSampleRate {
				t.Errorf("Expected SampleRate = %f, got %f", tt.expectedSampleRate, client.options.SampleRate)
			}
		})
	}
}

// TestSpotlightTracesSampleRateOverride is a regression test: Spotlight
// forced SampleRate to 1.0 but left TracesSampleRate untouched, so with
// tracing enabled and the default zero TracesSampleRate, transactions were
// silently dropped and never reached Spotlight despite the "always deliver
// 100%" intent.
func TestSpotlightTracesSampleRateOverride(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Spotlight:        true,
		EnableTracing:    true,
		TracesSampleRate: 0.1,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.options.TracesSampleRate != 1.0 {
		t.Errorf("Expected TracesSampleRate to be overridden to 1.0, got %f", client.options.TracesSampleRate)
	}
}

// TestSpotlightTracesSampleRateNotOverriddenWithoutTracing verifies the
// override in the previous test only applies when tracing is actually
// enabled, so Spotlight doesn't silently turn tracing on for users who never
// opted into it.
func TestSpotlightTracesSampleRateNotOverriddenWithoutTracing(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Spotlight: true,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.options.TracesSampleRate != 0 {
		t.Errorf("Expected TracesSampleRate to remain 0 without EnableTracing, got %f", client.options.TracesSampleRate)
	}
}

func TestSpotlightPIIOverride(t *testing.T) {
	tests := []struct {
		name            string
		inputSendPII    bool
		expectedSendPII bool
	}{
		{
			name:            "SendDefaultPII false overridden to true",
			inputSendPII:    false,
			expectedSendPII: true,
		},
		{
			name:            "SendDefaultPII true unchanged",
			inputSendPII:    true,
			expectedSendPII: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				Spotlight:      true,
				SendDefaultPII: tt.inputSendPII,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}

			if client.options.SendDefaultPII != tt.expectedSendPII {
				t.Errorf("Expected SendDefaultPII = %v, got %v", tt.expectedSendPII, client.options.SendDefaultPII)
			}
		})
	}
}

func TestSpotlightDisabledPreservesSettings(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Spotlight:      false,
		SampleRate:     0.5,
		SendDefaultPII: false,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.options.SampleRate != 0.5 {
		t.Errorf("Expected SampleRate = 0.5, got %f", client.options.SampleRate)
	}

	if client.options.SendDefaultPII {
		t.Errorf("Expected SendDefaultPII = false, got %v", client.options.SendDefaultPII)
	}
}

// TestSpotlightWithDSNPreservesSettings is a regression test for the spec
// requirement that Spotlight settings must not affect upstream Sentry
// configuration: the SampleRate/SendDefaultPII override is only meant as a
// fallback for Spotlight-only (no DSN) setups.
func TestSpotlightWithDSNPreservesSettings(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Dsn:            "https://user@sentry.io/123",
		Spotlight:      true,
		SampleRate:     0.5,
		SendDefaultPII: false,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if client.options.SampleRate != 0.5 {
		t.Errorf("Expected SampleRate to remain 0.5 with a DSN configured, got %f", client.options.SampleRate)
	}
	if client.options.SendDefaultPII {
		t.Errorf("Expected SendDefaultPII to remain false with a DSN configured, got %v", client.options.SendDefaultPII)
	}
}

// TestSpotlightPIIOverridePropagatesToDataCollection is a regression test:
// the Spotlight SendDefaultPII override used to run after DataCollection was
// already resolved from the pre-override value, so DataCollection (which
// takes precedence over SendDefaultPII wherever both are checked) kept the
// restrictive legacy defaults even though SendDefaultPII itself reported
// true. Spotlight-only mode must actually collect full PII, not just claim to.
func TestSpotlightPIIOverridePropagatesToDataCollection(t *testing.T) {
	client, err := NewClient(ClientOptions{
		Spotlight:      true,
		SendDefaultPII: false,
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if !client.options.SendDefaultPII {
		t.Fatalf("Expected SendDefaultPII to be overridden to true")
	}
	if client.options.DataCollection == nil {
		t.Fatalf("Expected DataCollection to be resolved")
	}
	if !client.options.DataCollection.UserInfo.Or(false) {
		t.Errorf("Expected DataCollection.UserInfo to reflect the overridden SendDefaultPII, got unset/false")
	}
	if len(client.options.DataCollection.HTTPBodies) == 0 {
		t.Errorf("Expected DataCollection.HTTPBodies to reflect the overridden SendDefaultPII, got none")
	}
}

// TestSpotlightIntegrationOnlyInstalledWhenEnabled is a regression test: the
// Spotlight integration was previously installed unconditionally, leaking
// "Spotlight" into every event's Sdk.Integrations even when Spotlight was
// never enabled.
func TestSpotlightIntegrationOnlyInstalledWhenEnabled(t *testing.T) {
	disabled, err := NewClient(ClientOptions{Dsn: "https://user@sentry.io/123"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if disabled.integrationAlreadyInstalled("Spotlight") {
		t.Errorf("Expected Spotlight integration not installed when Spotlight is disabled")
	}

	enabled, err := NewClient(ClientOptions{Spotlight: true})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if !enabled.integrationAlreadyInstalled("Spotlight") {
		t.Errorf("Expected Spotlight integration installed when Spotlight is enabled")
	}
}

// TestSpotlightUsesTelemetryProcessorByDefault verifies Spotlight is wired
// into the new internal/telemetry scheduler by default (not the legacy
// SpotlightTransport decorator), and that a captured event actually reaches
// the Spotlight sidecar through that path end-to-end.
func TestSpotlightUsesTelemetryProcessorByDefault(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if ct := r.Header.Get("Content-Type"); ct != "application/x-sentry-envelope" {
			t.Errorf("Expected application/x-sentry-envelope, got %s", ct)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{
		Spotlight:    true,
		SpotlightURL: server.URL + "/stream",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer client.Close()

	if client.telemetryProcessor == nil {
		t.Fatalf("Expected Spotlight to use the telemetry processor path by default")
	}
	if _, isDecorator := client.Transport.(*SpotlightTransport); isDecorator {
		t.Errorf("Expected client.Transport not to be SpotlightTransport when using the telemetry processor path")
	}

	client.CaptureMessage("test message", nil, nil)

	if !client.Flush(2 * time.Second) {
		t.Fatalf("Expected Flush to succeed")
	}
	if got := requestCount.Load(); got != 1 {
		t.Errorf("Expected 1 request to reach Spotlight via the telemetry processor, got %d", got)
	}
}

// TestSpotlightClientOptionsLegacyTransportPath verifies Spotlight still
// falls back to the SpotlightTransport decorator when a custom Transport (or
// DisableTelemetryBuffer) forces the legacy transport path.
func TestSpotlightClientOptionsLegacyTransportPath(t *testing.T) {
	tests := []struct {
		name    string
		options ClientOptions
	}{
		{
			name: "custom Transport",
			options: ClientOptions{
				Spotlight: true,
				Transport: &MockTransport{},
			},
		},
		{
			name: "DisableTelemetryBuffer",
			options: ClientOptions{
				Spotlight:              true,
				DisableTelemetryBuffer: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(tt.options)
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			defer client.Close()

			if client.telemetryProcessor != nil {
				t.Errorf("Expected legacy transport path, got telemetry processor")
			}
			if _, ok := client.Transport.(*SpotlightTransport); !ok {
				t.Errorf("Expected SpotlightTransport, got %T", client.Transport)
			}
		})
	}
}

func TestSpotlightProxyConfiguration(t *testing.T) {
	// Test with HTTPProxy option
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{
		Spotlight: true,
		HTTPProxy: "http://proxy.example.com:8080",
	})

	if st.client == nil {
		t.Errorf("Expected HTTP client to be configured")
	}

	transport, ok := st.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Expected *http.Transport, got %T", st.client.Transport)
	}

	if transport.Proxy == nil {
		t.Errorf("Expected Proxy to be configured")
	}
}

func TestSpotlightCustomHTTPClient(t *testing.T) {
	// Create a custom HTTP client
	customClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{
		Spotlight:  true,
		HTTPClient: customClient,
	})

	if st.client == nil {
		t.Errorf("Expected HTTP client to be configured")
	}

	// Spotlight enforces its own 5s timeout even when the caller supplies a longer one.
	if st.client.Timeout != 5*time.Second {
		t.Errorf("Expected timeout 5s for Spotlight, got %v", st.client.Timeout)
	}
}

func TestSpotlightAsyncSend(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		time.Sleep(100 * time.Millisecond) // Simulate slow server
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	start := time.Now()
	for i := 0; i < 5; i++ {
		event := NewEvent()
		event.Message = "Test message " + string(rune(i))
		st.SendEvent(event)
	}
	elapsed := time.Since(start)

	// Should return immediately, not wait for all sends to complete
	if elapsed > 500*time.Millisecond {
		t.Errorf("SendEvent took too long (%v), should be non-blocking", elapsed)
	}

	// The underlying transport is called synchronously by SendEvent, before
	// the Spotlight goroutine is spawned, so this assertion needs no sleep.
	if len(mock.Events()) != 5 {
		t.Errorf("Expected 5 events in mock, got %d", len(mock.Events()))
	}

	st.Flush(2 * time.Second)
	st.Close()
}

func TestSpotlightContextCancellation(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second) // Very slow server
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: slowServer.URL + "/stream"})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	// Close immediately while the slow server is still handling the request.
	// This should cancel the in-flight request rather than blocking for 5 seconds.
	st.Close()

	if st.ctx.Err() == nil {
		t.Errorf("Expected context to be cancelled after Close()")
	}
}

func TestSpotlightShutdownTimeout(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second) // Much longer than shutdown timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: slowServer.URL + "/stream"})

	// Send multiple events
	for i := 0; i < 3; i++ {
		event := NewEvent()
		event.Message = "Test message"
		st.SendEvent(event)
	}

	// Close should timeout gracefully
	start := time.Now()
	st.Close()
	elapsed := time.Since(start)

	// Should timeout after ~2 seconds, not hang
	if elapsed > 3*time.Second {
		t.Errorf("Close took too long (%v), should respect 2s timeout", elapsed)
	}
}

func TestSpotlightServerError(t *testing.T) {
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: errorServer.URL + "/stream"})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	// The underlying transport is called synchronously by SendEvent, before
	// the Spotlight goroutine is spawned, so this assertion needs no sleep.
	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event in mock, got %d", len(mock.Events()))
	}

	// Flush waits for the (failing) Spotlight send to finish, so Close below
	// has nothing left to wait out.
	st.Flush(2 * time.Second)
	st.Close()
}

func TestSpotlightNetworkError(t *testing.T) {
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{
		SpotlightURL: "http://localhost:54321", // Unreachable port
	})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	// The underlying transport is called synchronously by SendEvent, before
	// the Spotlight goroutine is spawned, so this assertion needs no sleep.
	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event in mock, got %d", len(mock.Events()))
	}

	st.Flush(2 * time.Second)
	st.Close()
}

// TestSpotlightConnectivityBackoffAndLogDedup is a regression test for the
// spec requirement that SDKs "MUST NOT log an error message for every failed
// envelope" and "SHOULD implement exponential backoff retry logic". It sends
// several events in quick succession against a server that always fails,
// and asserts both that the failure is only logged once and that the
// backoff window suppresses the follow-up HTTP attempts entirely.
func TestSpotlightConnectivityBackoffAndLogDedup(t *testing.T) {
	var requestCount atomic.Int32
	failingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failingServer.Close()

	var logBuf bytes.Buffer
	debuglog.SetOutput(&logBuf)
	defer debuglog.SetOutput(io.Discard)

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: failingServer.URL + "/stream"})
	defer st.Close()

	// First send establishes the failure and starts the backoff window.
	st.SendEvent(NewEvent())
	if !st.Flush(2 * time.Second) {
		t.Fatalf("Expected Flush to complete")
	}
	if got := requestCount.Load(); got != 1 {
		t.Fatalf("Expected exactly 1 request after the first send, got %d", got)
	}

	// Subsequent sends, issued sequentially while still within the backoff
	// window, must be dropped without hitting the server again.
	for i := 0; i < 4; i++ {
		st.SendEvent(NewEvent())
		if !st.Flush(2 * time.Second) {
			t.Fatalf("Expected Flush to complete")
		}
	}

	if got := requestCount.Load(); got != 1 {
		t.Errorf("Expected backoff to suppress subsequent requests, got %d total requests", got)
	}

	logged := strings.Count(logBuf.String(), "Failed to send to Spotlight")
	if logged != 1 {
		t.Errorf("Expected the connectivity failure to be logged exactly once, got %d occurrences in: %s", logged, logBuf.String())
	}
}

// TestSpotlightBackoffDoesNotCompoundAcrossConcurrentFailures is a regression
// test: sends happen one goroutine per event, so several can race past
// readyToSend before any of them advances the backoff window. Only the
// failure that actually starts (or re-starts) the outage should double the
// delay; concurrent stragglers reporting the same outage must not compound it
// (previously N concurrent failures multiplied the delay by 2^N).
func TestSpotlightBackoffDoesNotCompoundAcrossConcurrentFailures(t *testing.T) {
	st := NewSpotlightTransport(&MockTransport{})
	st.Configure(ClientOptions{SpotlightURL: "http://localhost:1"})
	defer st.Close()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.recordFailure(errors.New("simulated connectivity failure"))
		}()
	}
	wg.Wait()

	if st.backoff.retryDelay != 2*spotlightInitialRetryDelay {
		t.Errorf("Expected retryDelay to double exactly once across concurrent failures, got %v", st.backoff.retryDelay)
	}
}

func TestSpotlightSlowServer(t *testing.T) {
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowServer.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: slowServer.URL + "/stream"})

	start := time.Now()
	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)
	elapsed := time.Since(start)

	// SendEvent should return immediately, not wait for server response
	if elapsed > 100*time.Millisecond {
		t.Errorf("SendEvent should not block on slow server, took %v", elapsed)
	}

	st.Close()
}

func TestSpotlightMultipleEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	// Send multiple events concurrently
	for i := 0; i < 10; i++ {
		event := NewEvent()
		event.Message = "Test message " + string(rune(i))
		st.SendEvent(event)
	}

	st.Close()

	// All events should be sent to mock transport
	if len(mock.Events()) != 10 {
		t.Errorf("Expected 10 events in mock, got %d", len(mock.Events()))
	}
}

func TestSpotlightFlushWithContext(t *testing.T) {
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	result := st.FlushWithContext(ctx)
	if !result {
		t.Errorf("Expected FlushWithContext to succeed")
	}
}

// TestSpotlightFlushWaitsForInFlightSends is a regression test for a bug where
// Flush/FlushWithContext only waited on the underlying transport, ignoring
// in-flight Spotlight sends. This matters most in Spotlight-only mode (empty
// DSN, noop underlying transport), where the underlying Flush returns true
// instantly and Flush would otherwise be meaningless for making sure
// Spotlight actually received the event before the program exits.
func TestSpotlightFlushWaitsForInFlightSends(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate a slow sidecar so that, if Flush didn't actually wait,
		// the assertion below would observe received == false.
		time.Sleep(200 * time.Millisecond)
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// noopTransport simulates the empty-DSN, Spotlight-only configuration.
	st := NewSpotlightTransport(noopTransport{})
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	if !st.Flush(time.Second) {
		t.Errorf("Expected Flush to succeed")
	}
	if !received.Load() {
		t.Errorf("Expected Spotlight sidecar to have received the event by the time Flush returned")
	}
}

// TestSpotlightFlushWithContextWaitsForInFlightSends is a regression test for a bug where
// FlushWithContext only waited on the underlying transport, ignoring in-flight
// Spotlight sends. This matters most in Spotlight-only mode (empty DSN, noop
// underlying transport), where the underlying Flush returns true instantly and
// FlushWithContext would otherwise be meaningless for making sure Spotlight
// actually received the event before the program exits.
func TestSpotlightFlushWithContextWaitsForInFlightSends(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		received.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	st := NewSpotlightTransport(noopTransport{})
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	event := NewEvent()
	event.Message = "flush with context wait test"
	st.SendEvent(event)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if !st.FlushWithContext(ctx) {
		t.Errorf("Expected FlushWithContext to succeed")
	}
	if !received.Load() {
		t.Errorf("Expected Spotlight sidecar to have received the event by the time FlushWithContext returned")
	}
}

func TestSpotlightBuildHTTPClientWithTransport(t *testing.T) {
	customTransport := &http.Transport{}

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{
		HTTPTransport: customTransport,
	})

	if st.client == nil {
		t.Fatalf("Expected HTTP client to be configured")
	}
	if st.client.Transport != customTransport {
		t.Errorf("Expected custom transport to be used")
	}
}

func TestSpotlightSendEventCancelledContext(_ *testing.T) {
	// Test that sendToSpotlight skips when context is already cancelled
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: "http://localhost:54321/stream"})

	// Cancel context before calling sendToSpotlight directly
	st.cancel()

	event := NewEvent()
	event.Message = "cancelled event"
	// Call directly (bypassing the goroutine wrapper) to test the ctx.Done() check
	st.sendToSpotlight(event)
}

// TestSpotlightEnvelopeSenderClonesBeforeReturning is a regression test: the
// telemetry scheduler hands the same *protocol.Envelope pointer to both the
// real transport (which mutates it later, e.g. attaching a client report, on
// its own background worker) and spotlightEnvelopeSender.Send. If the clone
// for Spotlight happened inside the async goroutine instead of synchronously
// before Send returns, it would race with that later mutation. This verifies
// the clone is already taken by the time Send returns, by mutating the
// original envelope immediately afterward and checking Spotlight never sees
// the mutation.
func TestSpotlightEnvelopeSenderClonesBeforeReturning(t *testing.T) {
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := newSpotlightEnvelopeSender(ClientOptions{SpotlightURL: server.URL + "/stream"})
	defer sender.Close()

	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{EventID: "original"})
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: []byte(`{"message":"original"}`),
	})

	sender.Send(envelope)

	// Simulate the real transport's background worker mutating the shared
	// envelope after handing it off (e.g. AttachToEnvelope appending a
	// client report item), immediately after Send returns.
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeClientReport},
		Payload: []byte(`{"discarded_events":[]}`),
	})

	select {
	case body := <-bodies:
		if strings.Contains(string(body), "client_report") {
			t.Errorf("Expected Spotlight to receive a clone taken before the later mutation, got: %s", body)
		}
		if !strings.Contains(string(body), "original") {
			t.Errorf("Expected Spotlight to receive the original item, got: %s", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for Spotlight to receive the envelope")
	}
}

// TestSpotlightTransportConcurrentSendEventAndFlush is a regression test:
// SpotlightTransport.SendEvent is invoked directly by user code and can race
// with a concurrent Flush/Close call from another goroutine (this SDK is
// used from concurrent web server handlers). A sync.WaitGroup requires any
// Add starting from zero to happen-before the corresponding Wait, which this
// pattern can violate, up to a runtime "WaitGroup misuse" panic under
// -race. SpotlightTransport tracks in-flight sends with an atomic counter
// polled via util.WaitForZero instead, which has no such requirement.
func TestSpotlightTransportConcurrentSendEventAndFlush(_ *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})
	defer st.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.SendEvent(NewEvent())
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			st.Flush(200 * time.Millisecond)
		}()
	}
	wg.Wait()
}
