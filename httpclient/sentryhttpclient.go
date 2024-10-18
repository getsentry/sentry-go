// Package sentryhttpclient provides Sentry integration for Requests modules to enable distributed tracing between services.
// It is compatible with `net/http.RoundTripper`.
//
//	import sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
//
//	roundTrippper := sentryhttpclient.NewSentryRoundTripper(nil, nil)
//	client := &http.Client{
//		Transport: roundTripper,
//	}
//
//	request, err := client.Do(request)
package sentryhttpclient

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
)

// SentryRoundTripTracerOption provides a specific type in which defines the option for SentryRoundTripper.
type SentryRoundTripTracerOption func(*SentryRoundTripper)

// WithTags allows the RoundTripper to includes additional tags.
func WithTags(tags map[string]string) SentryRoundTripTracerOption {
	return func(t *SentryRoundTripper) {
		for k, v := range tags {
			t.tags[k] = v
		}
	}
}

// WithTag allows the RoundTripper to includes additional tag.
func WithTag(key, value string) SentryRoundTripTracerOption {
	return func(t *SentryRoundTripper) {
		t.tags[key] = value
	}
}

// NewSentryRoundTripper provides a wrapper to existing http.RoundTripper to have required span data and trace headers for outgoing HTTP requests.
//
//   - If `nil` is passed to `originalRoundTripper`, it will use http.DefaultTransport instead.
func NewSentryRoundTripper(originalRoundTripper http.RoundTripper, opts ...SentryRoundTripTracerOption) http.RoundTripper {
	if originalRoundTripper == nil {
		originalRoundTripper = http.DefaultTransport
	}

	t := &SentryRoundTripper{
		originalRoundTripper: originalRoundTripper,
		tags:                 make(map[string]string),
	}

	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}

	return t
}

// SentryRoundTripper provides a http.RoundTripper implementation for Sentry Requests module.
type SentryRoundTripper struct {
	originalRoundTripper http.RoundTripper

	tags map[string]string
}

func (s *SentryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	// Only create the `http.client` span only if there is a parent span.
	parentSpan := sentry.SpanFromContext(request.Context())
	if parentSpan == nil {
		return s.originalRoundTripper.RoundTrip(request)
	}

	cleanRequestURL := request.URL.Redacted()

	span := parentSpan.StartChild("http.client", sentry.WithTransactionName(fmt.Sprintf("%s %s", request.Method, cleanRequestURL)))
	span.Tags = s.tags
	defer span.Finish()

	span.SetData("http.query", request.URL.Query().Encode())
	span.SetData("http.fragment", request.URL.Fragment)
	span.SetData("http.request.method", request.Method)
	span.SetData("server.address", request.URL.Hostname())
	span.SetData("server.port", request.URL.Port())

	// Always add `Baggage` and `Sentry-Trace` headers.
	request.Header.Add("Baggage", span.ToBaggage())
	request.Header.Add("Sentry-Trace", span.ToSentryTrace())

	response, err := s.originalRoundTripper.RoundTrip(request)

	if response != nil {
		span.Status = sentry.HTTPtoSpanStatus(response.StatusCode)
		span.SetData("http.response.status_code", response.StatusCode)
		span.SetData("http.response_content_length", response.ContentLength)
	}

	return response, err
}

// SentryHTTPClient provides a default HTTP client with SentryRoundTripper included.
// This can be used directly to perform HTTP request.
var SentryHTTPClient = &http.Client{
	Transport: NewSentryRoundTripper(http.DefaultTransport),
}
