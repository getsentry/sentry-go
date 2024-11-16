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
	"strings"

	"github.com/getsentry/sentry-go"
)

// SentryRoundTripTracerOption provides a specific type in which defines the option for SentryRoundTripper.
type SentryRoundTripTracerOption func(*SentryRoundTripper)

// WithTracePropagationTargets configures additional trace propagation targets URL for the RoundTripper.
// Does not support regex patterns.
func WithTracePropagationTargets(targets []string) SentryRoundTripTracerOption {
	return func(t *SentryRoundTripper) {
		if t.tracePropagationTargets == nil {
			t.tracePropagationTargets = targets
		} else {
			t.tracePropagationTargets = append(t.tracePropagationTargets, targets...)
		}
	}
}

// NewSentryRoundTripper provides a wrapper to existing http.RoundTripper to have required span data and trace headers for outgoing HTTP requests.
//
//   - If `nil` is passed to `originalRoundTripper`, it will use http.DefaultTransport instead.
func NewSentryRoundTripper(originalRoundTripper http.RoundTripper, opts ...SentryRoundTripTracerOption) http.RoundTripper {
	if originalRoundTripper == nil {
		originalRoundTripper = http.DefaultTransport
	}

	// Configure trace propagation targets
	var tracePropagationTargets []string
	if hub := sentry.CurrentHub(); hub != nil {
		client := hub.Client()
		if client != nil {
			clientOptions := client.Options()
			if clientOptions.TracePropagationTargets != nil {
				tracePropagationTargets = clientOptions.TracePropagationTargets
			}

		}
	}

	t := &SentryRoundTripper{
		originalRoundTripper:    originalRoundTripper,
		tracePropagationTargets: tracePropagationTargets,
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

	tracePropagationTargets []string
}

func (s *SentryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	// Respect trace propagation targets
	if len(s.tracePropagationTargets) > 0 {
		requestURL := request.URL.String()
		foundMatch := false
		for _, target := range s.tracePropagationTargets {
			if strings.Contains(requestURL, target) {
				foundMatch = true
				break
			}
		}

		if !foundMatch {
			return s.originalRoundTripper.RoundTrip(request)
		}
	}

	// Only create the `http.client` span only if there is a parent span.
	parentSpan := sentry.SpanFromContext(request.Context())
	if parentSpan == nil {
		if hub := sentry.GetHubFromContext(request.Context()); hub != nil {
			request.Header.Add("Baggage", hub.GetBaggage())
			request.Header.Add("Sentry-Trace", hub.GetTraceparent())
		}

		return s.originalRoundTripper.RoundTrip(request)
	}

	cleanRequestURL := request.URL.Redacted()

	span := parentSpan.StartChild("http.client", sentry.WithTransactionName(fmt.Sprintf("%s %s", request.Method, cleanRequestURL)))
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
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	}

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
