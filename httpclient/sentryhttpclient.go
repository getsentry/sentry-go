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
	"github.com/getsentry/sentry-go/internal/httputils"
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
	var propagateTraceparent bool
	if hub := sentry.CurrentHub(); hub != nil {
		client := hub.Client()
		if client != nil {
			clientOptions := client.Options()
			if clientOptions.TracePropagationTargets != nil {
				tracePropagationTargets = clientOptions.TracePropagationTargets
			}
			propagateTraceparent = clientOptions.PropagateTraceparent
		}
	}

	t := &SentryRoundTripper{
		originalRoundTripper:    originalRoundTripper,
		tracePropagationTargets: tracePropagationTargets,
		propagateTraceparent:    propagateTraceparent,
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

	propagateTraceparent    bool
	tracePropagationTargets []string
}

func dataCollectionFromRequest(request *http.Request) sentry.DataCollection {
	if hub := sentry.GetHubFromContext(request.Context()); hub != nil {
		if client := hub.Client(); client != nil {
			return client.GetDataCollection()
		}
	}
	if hub := sentry.CurrentHub(); hub != nil {
		if client := hub.Client(); client != nil {
			return client.GetDataCollection()
		}
	}
	return sentry.DataCollection{}
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
			request = request.Clone(request.Context())
			request.Header.Add(sentry.SentryBaggageHeader, hub.GetBaggage())
			request.Header.Add(sentry.SentryTraceHeader, hub.GetTraceparent())
			if s.propagateTraceparent {
				request.Header.Add(sentry.TraceparentHeader, hub.GetTraceparentW3C())
			}
		}

		return s.originalRoundTripper.RoundTrip(request)
	}

	dc := dataCollectionFromRequest(request)
	cleanRequestURL := dc.FilterURL(request.URL)

	span := parentSpan.StartChild("http.client", sentry.WithDescription(fmt.Sprintf("%s %s", request.Method, cleanRequestURL)))
	defer span.Finish()

	if dc.CollectQueryParams() {
		span.SetData("http.query", dc.FilterQueryString(request.URL.RawQuery))
	}
	span.SetData("http.fragment", request.URL.Fragment)
	span.SetData("http.request.method", request.Method)
	span.SetData("server.address", request.URL.Hostname())
	span.SetData("server.port", request.URL.Port())
	for key, value := range dc.FilterRequestHeaders(headerStringMap(request.Header)) {
		span.SetData("http.request.header."+strings.ToLower(key), value)
	}
	if dc.CollectHTTPBody(sentry.BodyOutgoingRequest) {
		if body := readOutgoingRequestBody(request); body != nil {
			span.SetData("http.request.body", dc.FilterHTTPBody(body, request.Header.Get("Content-Type")))
		}
	}

	// Always add `Baggage` and `Sentry-Trace` headers.
	request = request.Clone(request.Context())
	request.Header.Add(sentry.SentryBaggageHeader, span.ToBaggage())
	request.Header.Add(sentry.SentryTraceHeader, span.ToSentryTrace())
	if s.propagateTraceparent {
		request.Header.Add(sentry.TraceparentHeader, span.ToTraceparent())
	}

	response, err := s.originalRoundTripper.RoundTrip(request)
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return response, err
	}

	if response != nil {
		span.Status = sentry.HTTPtoSpanStatus(response.StatusCode)
		span.SetData("http.response.status_code", response.StatusCode)
		span.SetData("http.response_content_length", response.ContentLength)
		for key, value := range dc.FilterResponseHeaders(headerStringMap(response.Header)) {
			span.SetData("http.response.header."+strings.ToLower(key), value)
		}
	}

	return response, err
}

// headerStringMap flattens HTTP headers into a map.
func headerStringMap(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}
	m := make(map[string]string, len(h))
	for key, values := range h {
		m[key] = strings.Join(values, ",")
	}
	return m
}

// readOutgoingRequestBody returns a copy of the outgoing request body via
// request.GetBody so the actual request stream sent to the server is left
// untouched.
func readOutgoingRequestBody(request *http.Request) []byte {
	if request.GetBody == nil {
		return nil
	}
	rc, err := request.GetBody()
	if err != nil || rc == nil {
		return nil
	}
	defer rc.Close()
	return httputils.ReadBody(rc)
}
