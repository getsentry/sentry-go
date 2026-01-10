package sentry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
