package sentryotlp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const apiVersion = "7"

// Option configures safe OTLP HTTP client behavior for the Sentry exporter.
// Target selection and auth remain derived from the DSN.
type Option func(*config)

type config struct {
	otlpOptions []otlptracehttp.Option
}

// WithCompression configures OTLP payload compression.
func WithCompression(compression otlptracehttp.Compression) Option {
	return func(c *config) {
		c.otlpOptions = append(c.otlpOptions, otlptracehttp.WithCompression(compression))
	}
}

// WithTLSClientConfig configures the HTTP client's TLS settings.
func WithTLSClientConfig(tlsCfg *tls.Config) Option {
	return func(c *config) {
		c.otlpOptions = append(c.otlpOptions, otlptracehttp.WithTLSClientConfig(tlsCfg))
	}
}

// WithTimeout configures the OTLP export timeout.
func WithTimeout(duration time.Duration) Option {
	return func(c *config) {
		c.otlpOptions = append(c.otlpOptions, otlptracehttp.WithTimeout(duration))
	}
}

// WithRetry configures OTLP retry behavior.
func WithRetry(rc otlptracehttp.RetryConfig) Option {
	return func(c *config) {
		c.otlpOptions = append(c.otlpOptions, otlptracehttp.WithRetry(rc))
	}
}

type sentryOTLPExporter struct {
	inner sdktrace.SpanExporter
}

// NewTraceExporter creates a new SpanExporter that sends spans to Sentry via the OTLP HTTP protocol.
//
// The endpoint, URL path, headers, and HTTP/HTTPS mode are derived from the DSN.
func NewTraceExporter(ctx context.Context, dsn string, opts ...Option) (sdktrace.SpanExporter, error) {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	otlpOpts, err := buildOTLPOptions(dsn, cfg.otlpOptions...)
	if err != nil {
		return nil, err
	}

	inner, err := otlptracehttp.New(ctx, otlpOpts...)
	if err != nil {
		return nil, fmt.Errorf("sentryotlp: failed to create OTLP exporter: %w", err)
	}

	return &sentryOTLPExporter{inner: inner}, nil
}

func buildOTLPOptions(dsn string, opts ...otlptracehttp.Option) ([]otlptracehttp.Option, error) {
	if dsn == "" {
		return nil, errors.New("sentryotlp: dsn must be provided")
	}

	parsedDSN, err := sentry.NewDsn(dsn)
	if err != nil {
		return nil, fmt.Errorf("sentryotlp: invalid DSN: %w", err)
	}

	otlpURL := otlpTracesURL(parsedDSN)
	otlpOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(otlpURL.Host),
		otlptracehttp.WithURLPath(otlpURL.EscapedPath()),
		otlptracehttp.WithHeaders(sentryAuthHeaders(parsedDSN)),
	}
	if otlpURL.Scheme == "http" {
		otlpOpts = append(otlpOpts, otlptracehttp.WithInsecure())
	}
	otlpOpts = append(otlpOpts, opts...)
	return otlpOpts, nil
}

func otlpTracesURL(dsn *sentry.Dsn) *url.URL {
	apiURL := dsn.GetAPIURL()
	apiURL.Path = strings.TrimSuffix(apiURL.Path, "/envelope/") + "/integration/otlp/v1/traces/"
	return apiURL
}

// ExportSpans exports a batch of spans to Sentry via OTLP HTTP.
func (e *sentryOTLPExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return e.inner.ExportSpans(ctx, spans)
}

// Shutdown shuts down the exporter, flushing any remaining spans.
func (e *sentryOTLPExporter) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}

// sentryAuthHeaders builds the X-Sentry-Auth header map for OTLP requests.
func sentryAuthHeaders(dsn *sentry.Dsn) map[string]string {
	auth := fmt.Sprintf(
		"Sentry sentry_version=%s, sentry_client=sentry.go/%s, sentry_key=%s",
		apiVersion, sentry.SDKVersion, dsn.GetPublicKey(),
	)
	return map[string]string{
		"X-Sentry-Auth": auth,
	}
}
