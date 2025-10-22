package sentry

import (
	"errors"
	"testing"
)

// TestDsn_Wrapper tests that the top-level Dsn wrapper works correctly.
func TestDsn_Wrapper(t *testing.T) {
	t.Run("initialized DSN", func(t *testing.T) {
		dsn, err := NewDsn("https://public:secret@example.com/1")
		if err != nil {
			t.Fatalf("NewDsn() failed: %v", err)
		}

		// Test that all methods are accessible and return expected values
		if dsn.String() == "" {
			t.Error("String() returned empty")
		}
		if dsn.GetHost() != "example.com" {
			t.Errorf("GetHost() = %s, want example.com", dsn.GetHost())
		}
		if dsn.GetPublicKey() != "public" {
			t.Errorf("GetPublicKey() = %s, want public", dsn.GetPublicKey())
		}
		if dsn.GetSecretKey() != "secret" {
			t.Errorf("GetSecretKey() = %s, want secret", dsn.GetSecretKey())
		}
		if dsn.GetProjectID() != "1" {
			t.Errorf("GetProjectID() = %s, want 1", dsn.GetProjectID())
		}
		if dsn.GetScheme() != "https" {
			t.Errorf("GetScheme() = %s, want https", dsn.GetScheme())
		}
		if dsn.GetPort() != 443 {
			t.Errorf("GetPort() = %d, want 443", dsn.GetPort())
		}
		if dsn.GetPath() != "" {
			t.Errorf("GetPath() = %s, want empty", dsn.GetPath())
		}
		if dsn.GetAPIURL() == nil {
			t.Error("GetAPIURL() returned nil")
		}
		if dsn.RequestHeaders() == nil {
			t.Error("RequestHeaders() returned nil")
		}
	})

	t.Run("empty DSN struct", func(t *testing.T) {
		var dsn Dsn // Zero-value struct

		// Test that all methods work without panicking
		// They should return empty/zero values for an uninitialized struct
		_ = dsn.String()
		_ = dsn.GetHost()
		_ = dsn.GetPublicKey()
		_ = dsn.GetSecretKey()
		_ = dsn.GetProjectID()
		_ = dsn.GetScheme()
		_ = dsn.GetPort()
		_ = dsn.GetPath()
		_ = dsn.GetAPIURL()
		_ = dsn.RequestHeaders()

		// If we get here without panicking, the test passes
		t.Log("All methods executed without panic on empty DSN struct")
	})

	t.Run("NewDsn error handling", func(t *testing.T) {
		_, err := NewDsn("invalid-dsn")
		if err == nil {
			t.Error("NewDsn() should return error for invalid DSN")
		}

		// Test that the error is the expected type
		var dsnParseError *DsnParseError
		if !errors.As(err, &dsnParseError) {
			t.Errorf("Expected *DsnParseError, got %T", err)
		}
	})
}
