package sentry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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
		if ua := r.Header.Get("User-Agent"); ua != "sentry-go/"+SDKVersion {
			t.Errorf("Expected sentry-go/%s, got %s", SDKVersion, ua)
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

	time.Sleep(100 * time.Millisecond)

	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event, got %d", len(mock.Events()))
	}
	if mock.Events()[0].Message != "Test message" {
		t.Errorf("Expected 'Test message', got %s", mock.Events()[0].Message)
	}

	if !st.Flush(time.Second) {
		t.Errorf("Expected Flush to succeed")
	}

	if mock.FlushCount() != 1 {
		t.Errorf("Expected underlying transport Flush called 1 time, got %d", mock.FlushCount())
	}
}

func TestSpotlightTransportWithNoopUnderlying(_ *testing.T) {
	// Mock Spotlight server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	st := NewSpotlightTransport(noopTransport{})
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)
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

			_, isSpotlight := client.Transport.(*SpotlightTransport)
			if isSpotlight != tt.hasSpotlight {
				t.Errorf("Expected SpotlightTransport = %v, got %v", tt.hasSpotlight, isSpotlight)
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

			st, ok := client.Transport.(*SpotlightTransport)
			if !ok {
				t.Fatalf("Expected SpotlightTransport, got %T", client.Transport)
			}

			if st.spotlightURL != tt.wantURL {
				t.Errorf("%s: Expected URL %s, got %s", tt.description, tt.wantURL, st.spotlightURL)
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

func TestCloneAndModifyEnvelopeForSpotlight(t *testing.T) {
	// Create a test envelope with an event
	event := &Event{
		EventID: EventID("test123"),
		Message: "Test event",
		Level:   LevelInfo,
	}

	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{
		EventID: "test123",
	})

	// Serialize event to JSON
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Add event item to envelope
	envelope.AddItem(&protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type: protocol.EnvelopeItemTypeEvent,
		},
		Payload: eventJSON,
	})

	// Add an attachment item (should be copied as-is)
	length := 5
	envelope.AddItem(&protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type:     protocol.EnvelopeItemTypeAttachment,
			Length:   &length,
			Filename: "test.txt",
		},
		Payload: []byte("hello"),
	})

	cloned := cloneEnvelopeForSpotlight(envelope)

	// Verify cloned envelope has same number of items
	if len(cloned.Items) != len(envelope.Items) {
		t.Errorf("Expected %d items, got %d", len(envelope.Items), len(cloned.Items))
	}

	// Verify event item was processed
	if cloned.Items[0].Header.Type != protocol.EnvelopeItemTypeEvent {
		t.Errorf("Expected event item type, got %s", cloned.Items[0].Header.Type)
	}

	// Verify attachment item was copied
	if cloned.Items[1].Header.Type != protocol.EnvelopeItemTypeAttachment {
		t.Errorf("Expected attachment item type, got %s", cloned.Items[1].Header.Type)
	}
	if cloned.Items[1].Header.Filename != "test.txt" {
		t.Errorf("Expected filename test.txt, got %s", cloned.Items[1].Header.Filename)
	}

	// Verify original envelope is unchanged
	if envelope.Items[0].Header.Type != protocol.EnvelopeItemTypeEvent {
		t.Errorf("Original envelope was modified")
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

	time.Sleep(1 * time.Second)
	if len(mock.Events()) != 5 {
		t.Errorf("Expected 5 events in mock, got %d", len(mock.Events()))
	}
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

	time.Sleep(500 * time.Millisecond) // Wait for async send

	// Should have sent to mock transport even if Spotlight fails
	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event in mock, got %d", len(mock.Events()))
	}

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

	time.Sleep(500 * time.Millisecond) // Wait for async send attempt

	// Should have sent to mock transport even if Spotlight is unreachable
	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event in mock, got %d", len(mock.Events()))
	}

	st.Close()
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

func TestSpotlightSendEnvelope(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	// Build and send an envelope
	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{
		EventID: "test-envelope-123",
	})
	event := &Event{
		EventID: "test-envelope-123",
		Message: "Envelope test",
		Level:   LevelError,
	}
	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: eventJSON,
	})

	st.SendEnvelope(envelope)
	time.Sleep(200 * time.Millisecond) // Wait for async send

	if len(mock.Events()) != 1 {
		t.Errorf("Expected 1 event in mock (from envelope), got %d", len(mock.Events()))
	}
	if requestCount.Load() != 1 {
		t.Errorf("Expected 1 request to Spotlight, got %d", requestCount.Load())
	}

	st.Close()
}

func TestSpotlightSendEnvelopeEmpty(_ *testing.T) {
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: "http://localhost:54321/stream"})

	emptyEnvelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{})
	st.SendEnvelope(emptyEnvelope)
	time.Sleep(100 * time.Millisecond)

	st.Close()
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

func TestSpotlightSendEnvelopeWithSDK(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{
		EventID: "test-sdk-123",
		Sdk: &protocol.SdkInfo{
			Name:         "sentry-go",
			Version:      SDKVersion,
			Integrations: []string{"spotlight"},
		},
	})
	event := &Event{
		EventID: "test-sdk-123",
		Message: "SDK envelope test",
	}
	eventJSON, _ := json.Marshal(event)
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: eventJSON,
	})

	st.SendEnvelope(envelope)
	time.Sleep(200 * time.Millisecond)
	st.Close()

	if requestCount.Load() != 1 {
		t.Errorf("Expected 1 request to Spotlight, got %d", requestCount.Load())
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

func TestSpotlightEnvelopeCancelledContext(_ *testing.T) {
	// Test that sendToSpotlightServer skips when context is already cancelled
	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: "http://localhost:54321/stream"})

	// Cancel the context before sending
	st.cancel()

	// Build an envelope with an event
	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{EventID: "test-cancel"})
	event := &Event{EventID: "test-cancel", Message: "cancelled"}
	eventJSON, _ := json.Marshal(event)
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: eventJSON,
	})

	// Directly call sendEnvelopeToSpotlight — context already cancelled, should skip gracefully
	st.sendEnvelopeToSpotlight(envelope)
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

func TestSpotlightSendEnvelopeServerError(_ *testing.T) {
	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer errorServer.Close()

	mock := &MockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: errorServer.URL + "/stream"})

	envelope := protocol.NewEnvelope(&protocol.EnvelopeHeader{EventID: "test-server-error"})
	event := &Event{EventID: "test-server-error", Message: "server error test"}
	eventJSON, _ := json.Marshal(event)
	envelope.AddItem(&protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: eventJSON,
	})

	st.SendEnvelope(envelope)
	time.Sleep(200 * time.Millisecond)
	st.Close()
}
