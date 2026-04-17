package sentryotlp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

func TestOTLPTracesURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		{
			name:     "standard SaaS DSN",
			dsn:      "https://key@o123.ingest.sentry.io/456",
			expected: "https://o123.ingest.sentry.io/api/456/integration/otlp/v1/traces/",
		},
		{
			name:     "self-hosted with custom port",
			dsn:      "https://key@sentry.example.com:9000/789",
			expected: "https://sentry.example.com:9000/api/789/integration/otlp/v1/traces/",
		},
		{
			name:     "self-hosted with path prefix",
			dsn:      "https://key@sentry.example.com/prefix/123",
			expected: "https://sentry.example.com/prefix/api/123/integration/otlp/v1/traces/",
		},
		{
			name:     "http scheme with default port",
			dsn:      "http://key@localhost/1",
			expected: "http://localhost/api/1/integration/otlp/v1/traces/",
		},
		{
			name:     "http with non-default port",
			dsn:      "http://key@localhost:8080/1",
			expected: "http://localhost:8080/api/1/integration/otlp/v1/traces/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dsn, err := sentry.NewDsn(tt.dsn)
			if err != nil {
				t.Fatalf("failed to parse DSN: %v", err)
			}
			got := otlpTracesURL(dsn).String()
			if got != tt.expected {
				t.Errorf("otlpTracesURL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSentryAuthHeaders(t *testing.T) {
	t.Parallel()

	dsn, err := sentry.NewDsn("https://mykey@o123.ingest.sentry.io/456")
	if err != nil {
		t.Fatalf("failed to parse DSN: %v", err)
	}

	headers := sentryAuthHeaders(dsn)

	auth, ok := headers["X-Sentry-Auth"]
	if !ok {
		t.Fatal("expected X-Sentry-Auth header")
	}

	if !strings.Contains(auth, "sentry_key=mykey") {
		t.Errorf("auth header missing sentry_key: %s", auth)
	}
	if !strings.Contains(auth, "sentry_version=7") {
		t.Errorf("auth header missing sentry_version: %s", auth)
	}
	if !strings.Contains(auth, "sentry_client=sentry.go/") {
		t.Errorf("auth header missing sentry_client: %s", auth)
	}
}

func TestNewSpanExporter_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("no DSN", func(t *testing.T) {
		t.Parallel()
		_, err := NewTraceExporter(ctx, "")
		if err == nil {
			t.Fatal("expected error when no DSN provided")
		}
		if !strings.Contains(err.Error(), "dsn must be provided") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("invalid DSN", func(t *testing.T) {
		t.Parallel()
		_, err := NewTraceExporter(ctx, "not-a-valid-dsn")
		if err == nil {
			t.Fatal("expected error for invalid DSN")
		}
		if !strings.Contains(err.Error(), "invalid DSN") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestBuildOTLPOptions_WithDSN(t *testing.T) {
	t.Parallel()

	opts, err := buildOTLPOptions("https://testkey@o123.ingest.sentry.io/789")
	if err != nil {
		t.Fatalf("buildOTLPOptions() error = %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("expected otlp options")
	}
}

func TestBuildOTLPOptions_WithAdditionalClientOptions(t *testing.T) {
	t.Parallel()

	cfg := &config{}
	WithTimeout(3 * time.Second)(cfg)
	WithCompression(otlptracehttp.GzipCompression)(cfg)

	opts, err := buildOTLPOptions("https://testkey@o123.ingest.sentry.io/789", cfg.otlpOptions...)
	if err != nil {
		t.Fatalf("buildOTLPOptions() error = %v", err)
	}
	if len(opts) < 4 {
		t.Fatalf("expected defaults plus custom options, got %d", len(opts))
	}
}
