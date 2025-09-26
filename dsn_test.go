package sentry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNewDsn_TopLevel(t *testing.T) {
	tests := []struct {
		name      string
		rawURL    string
		wantError bool
	}{
		{
			name:      "valid HTTPS DSN",
			rawURL:    "https://public@example.com/1",
			wantError: false,
		},
		{
			name:      "valid HTTP DSN",
			rawURL:    "http://public@example.com/1",
			wantError: false,
		},
		{
			name:      "DSN with secret",
			rawURL:    "https://public:secret@example.com/1",
			wantError: false,
		},
		{
			name:      "DSN with path",
			rawURL:    "https://public@example.com/path/to/project/1",
			wantError: false,
		},
		{
			name:      "DSN with port",
			rawURL:    "https://public@example.com:3000/1",
			wantError: false,
		},
		{
			name:      "invalid DSN - no project ID",
			rawURL:    "https://public@example.com/",
			wantError: true,
		},
		{
			name:      "invalid DSN - no host",
			rawURL:    "https://public@/1",
			wantError: true,
		},
		{
			name:      "invalid DSN - no public key",
			rawURL:    "https://example.com/1",
			wantError: true,
		},
		{
			name:      "invalid DSN - malformed URL",
			rawURL:    "not-a-url",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := NewDsn(tt.rawURL)

			if (err != nil) != tt.wantError {
				t.Errorf("NewDsn() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err != nil {
				return // Expected error, nothing more to check
			}

			// Basic validation for successful cases
			if dsn == nil {
				t.Error("NewDsn() returned nil DSN")
				return
			}

			if dsn.Dsn == nil {
				t.Error("NewDsn() returned DSN with nil internal Dsn")
				return
			}

			// Verify the DSN can be converted back to string
			dsnString := dsn.String()
			if dsnString == "" {
				t.Error("DSN String() returned empty string")
			}

			// Verify basic getters work
			if dsn.GetProjectID() == "" {
				t.Error("DSN GetProjectID() returned empty string")
			}

			if dsn.GetHost() == "" {
				t.Error("DSN GetHost() returned empty string")
			}

			if dsn.GetPublicKey() == "" {
				t.Error("DSN GetPublicKey() returned empty string")
			}
		})
	}
}

func TestDsn_RequestHeaders_TopLevel(t *testing.T) {
	tests := []struct {
		name      string
		dsnString string
	}{
		{
			name:      "DSN without secret key",
			dsnString: "https://public@example.com/1",
		},
		{
			name:      "DSN with secret key",
			dsnString: "https://public:secret@example.com/1",
		},
		{
			name:      "DSN with path",
			dsnString: "https://public@example.com/path/to/project/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := NewDsn(tt.dsnString)
			if err != nil {
				t.Fatalf("NewDsn() error = %v", err)
			}

			headers := dsn.RequestHeaders()

			// Verify required headers are present
			if headers["Content-Type"] != "application/json" {
				t.Errorf("Content-Type = %s, want application/json", headers["Content-Type"])
			}

			authHeader, exists := headers["X-Sentry-Auth"]
			if !exists {
				t.Error("X-Sentry-Auth header missing")
				return
			}

			// Verify auth header contains expected components
			expectedComponents := []string{
				"Sentry sentry_version=7",
				"sentry_client=sentry.go/" + SDKVersion,
				"sentry_key=" + dsn.GetPublicKey(),
				"sentry_timestamp=",
			}

			for _, component := range expectedComponents {
				if !strings.Contains(authHeader, component) {
					t.Errorf("X-Sentry-Auth missing component: %s, got: %s", component, authHeader)
				}
			}

			// Check for secret key if present
			if dsn.GetSecretKey() != "" {
				secretComponent := "sentry_secret=" + dsn.GetSecretKey()
				if !strings.Contains(authHeader, secretComponent) {
					t.Errorf("X-Sentry-Auth missing secret component: %s", secretComponent)
				}
			}
		})
	}
}

func TestDsn_MarshalJSON_TopLevel(t *testing.T) {
	tests := []struct {
		name      string
		dsnString string
	}{
		{
			name:      "basic DSN",
			dsnString: "https://public@example.com/1",
		},
		{
			name:      "DSN with secret",
			dsnString: "https://public:secret@example.com/1",
		},
		{
			name:      "DSN with path",
			dsnString: "https://public@example.com/path/to/project/1",
		},
		{
			name:      "DSN with port",
			dsnString: "https://public@example.com:3000/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := NewDsn(tt.dsnString)
			if err != nil {
				t.Fatalf("NewDsn() error = %v", err)
			}

			data, err := dsn.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
				return
			}

			// Should be valid JSON
			var result string
			if err := json.Unmarshal(data, &result); err != nil {
				t.Errorf("Marshaled data is not valid JSON: %v", err)
				return
			}

			// The result should be the DSN string
			if result != dsn.String() {
				t.Errorf("MarshalJSON() = %s, want %s", result, dsn.String())
			}
		})
	}
}

func TestDsn_UnmarshalJSON_TopLevel(t *testing.T) {
	tests := []struct {
		name      string
		jsonData  string
		wantError bool
	}{
		{
			name:      "valid DSN JSON",
			jsonData:  `"https://public@example.com/1"`,
			wantError: false,
		},
		{
			name:      "valid DSN with secret",
			jsonData:  `"https://public:secret@example.com/1"`,
			wantError: false,
		},
		{
			name:      "valid DSN with path",
			jsonData:  `"https://public@example.com/path/to/project/1"`,
			wantError: false,
		},
		{
			name:      "invalid DSN JSON",
			jsonData:  `"invalid-dsn"`,
			wantError: true,
		},
		{
			name:      "empty string JSON",
			jsonData:  `""`,
			wantError: true,
		},
		{
			name:      "malformed JSON",
			jsonData:  `invalid-json`,
			wantError: true, // UnmarshalJSON will try to parse as DSN and fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var dsn Dsn
			err := dsn.UnmarshalJSON([]byte(tt.jsonData))

			if (err != nil) != tt.wantError {
				t.Errorf("UnmarshalJSON() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err == nil && strings.HasPrefix(tt.jsonData, `"`) && strings.HasSuffix(tt.jsonData, `"`) {
				// For valid JSON string cases, verify the DSN was properly reconstructed
				var expectedDsnString string
				if unmarshErr := json.Unmarshal([]byte(tt.jsonData), &expectedDsnString); unmarshErr != nil {
					t.Errorf("json.Unmarshal failed: %v", unmarshErr)
				} else if dsn.String() != expectedDsnString {
					t.Errorf("UnmarshalJSON() result = %s, want %s", dsn.String(), expectedDsnString)
				}
			}
		})
	}
}

func TestDsn_MarshalUnmarshal_RoundTrip_TopLevel(t *testing.T) {
	originalDsnString := "https://public:secret@example.com:3000/path/to/project/1"

	// Create original DSN
	originalDsn, err := NewDsn(originalDsnString)
	if err != nil {
		t.Fatalf("NewDsn() error = %v", err)
	}

	// Marshal to JSON
	data, err := originalDsn.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}

	// Unmarshal from JSON
	var reconstructedDsn Dsn
	err = reconstructedDsn.UnmarshalJSON(data)
	if err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}

	// Compare string representations
	if originalDsn.String() != reconstructedDsn.String() {
		t.Errorf("Round trip failed: %s != %s", originalDsn.String(), reconstructedDsn.String())
	}

	// Compare all individual fields to ensure integrity
	if originalDsn.GetScheme() != reconstructedDsn.GetScheme() {
		t.Errorf("Scheme mismatch: %s != %s", originalDsn.GetScheme(), reconstructedDsn.GetScheme())
	}
	if originalDsn.GetPublicKey() != reconstructedDsn.GetPublicKey() {
		t.Errorf("PublicKey mismatch: %s != %s", originalDsn.GetPublicKey(), reconstructedDsn.GetPublicKey())
	}
	if originalDsn.GetSecretKey() != reconstructedDsn.GetSecretKey() {
		t.Errorf("SecretKey mismatch: %s != %s", originalDsn.GetSecretKey(), reconstructedDsn.GetSecretKey())
	}
	if originalDsn.GetHost() != reconstructedDsn.GetHost() {
		t.Errorf("Host mismatch: %s != %s", originalDsn.GetHost(), reconstructedDsn.GetHost())
	}
	if originalDsn.GetPort() != reconstructedDsn.GetPort() {
		t.Errorf("Port mismatch: %d != %d", originalDsn.GetPort(), reconstructedDsn.GetPort())
	}
	if originalDsn.GetPath() != reconstructedDsn.GetPath() {
		t.Errorf("Path mismatch: %s != %s", originalDsn.GetPath(), reconstructedDsn.GetPath())
	}
	if originalDsn.GetProjectID() != reconstructedDsn.GetProjectID() {
		t.Errorf("ProjectID mismatch: %s != %s", originalDsn.GetProjectID(), reconstructedDsn.GetProjectID())
	}
}

func TestDsnParseError_Compatibility(t *testing.T) {
	// Test that the re-exported DsnParseError works as expected
	_, err := NewDsn("invalid-dsn")
	if err == nil {
		t.Error("Expected error for invalid DSN")
		return
	}

	// Verify it's the expected error type
	if _, ok := err.(*DsnParseError); !ok {
		t.Errorf("Expected DsnParseError, got %T", err)
	}

	// Verify error message format
	errorMsg := err.Error()
	if !strings.Contains(errorMsg, "[Sentry] DsnParseError:") {
		t.Errorf("Unexpected error message format: %s", errorMsg)
	}
}
