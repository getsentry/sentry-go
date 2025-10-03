package sentry

import (
	"context"
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

	mock := &mockTransport{}
	st := NewSpotlightTransport(mock)
	st.Configure(ClientOptions{SpotlightURL: server.URL + "/stream"})

	event := NewEvent()
	event.Message = "Test message"
	st.SendEvent(event)

	time.Sleep(100 * time.Millisecond)

	if len(mock.events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(mock.events))
	}
	if mock.events[0].Message != "Test message" {
		t.Errorf("Expected 'Test message', got %s", mock.events[0].Message)
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

// mockTransport is a simple transport for testing.
type mockTransport struct {
	events []*Event
}

func (m *mockTransport) Configure(ClientOptions) {}

func (m *mockTransport) SendEvent(event *Event) {
	m.events = append(m.events, event)
}

func (m *mockTransport) Flush(time.Duration) bool {
	return true
}

func (m *mockTransport) FlushWithContext(_ context.Context) bool {
	return true
}

func (m *mockTransport) Close() {}
