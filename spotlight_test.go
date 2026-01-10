package sentry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// Clone and modify
	st := NewSpotlightTransport(&MockTransport{})
	cloned := st.cloneAndModifyEnvelopeForSpotlight(envelope)

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

func TestModifyEventItemForSpotlight(t *testing.T) {
	// Create an event
	event := &Event{
		EventID: EventID("test123"),
		Message: "Test event",
		Level:   LevelError,
	}

	eventJSON, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	item := &protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type: protocol.EnvelopeItemTypeEvent,
		},
		Payload: eventJSON,
	}

	st := NewSpotlightTransport(&MockTransport{})
	modifiedItem := st.modifyEventItemForSpotlight(item)

	// Deserialize the modified payload
	var modifiedEvent Event
	err = json.Unmarshal(modifiedItem.Payload, &modifiedEvent)
	if err != nil {
		t.Fatalf("Failed to unmarshal modified event: %v", err)
	}

	// Verify event data is preserved
	if modifiedEvent.Message != "Test event" {
		t.Errorf("Expected message 'Test event', got %q", modifiedEvent.Message)
	}
	if modifiedEvent.Level != LevelError {
		t.Errorf("Expected level Error, got %v", modifiedEvent.Level)
	}

	// Verify payload is not empty
	if len(modifiedItem.Payload) == 0 {
		t.Errorf("Expected non-empty payload after modification")
	}
}

func TestModifyEventItemForSpotlightInvalidPayload(t *testing.T) {
	// Create item with invalid JSON payload
	item := &protocol.EnvelopeItem{
		Header: &protocol.EnvelopeItemHeader{
			Type: protocol.EnvelopeItemTypeEvent,
		},
		Payload: []byte("invalid json {"),
	}

	st := NewSpotlightTransport(&MockTransport{})
	modifiedItem := st.modifyEventItemForSpotlight(item)

	// Should return original item on error
	if !bytesEqual(modifiedItem.Payload, item.Payload) {
		t.Errorf("Expected original payload to be returned on deserialization error")
	}
}

// Helper to compare byte slices
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
