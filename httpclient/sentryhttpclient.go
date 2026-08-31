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
	"context"
	"fmt"
	"io"
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
			t.tracePropagationTargets = append([]string(nil), targets...)
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

	t := &SentryRoundTripper{
		originalRoundTripper: originalRoundTripper,
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

func dataCollectionFromRequest(request *http.Request) sentry.DataCollection {
	client := sentry.GetClient(request.Context())
	if client.IsEnabled() {
		return client.GetDataCollection()
	}
	return sentry.DataCollection{}
}

func (s *SentryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	client := sentry.GetClient(ctx)
	clientOptions := client.Options()
	propagate := s.shouldPropagate(request.URL.String(), clientOptions.TracePropagationTargets)

	// Only create the `http.client` span only if there is a parent span.
	parentSpan := sentry.SpanFromContext(ctx)
	if parentSpan == nil {
		if propagate {
			request = addTraceHeaders(ctx, request, clientOptions.PropagateTraceparent)
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
	for key, value := range filterOutgoingRequestHeaders(dc, request.Header) {
		span.SetData("http.request.header."+strings.ToLower(key), value)
	}
	if propagate {
		request = addTraceHeaders(span.Context(), request, clientOptions.PropagateTraceparent)
	}

	var requestBody *httputils.LimitedBuffer
	hasRequestBody := request.Body != nil && request.Body != http.NoBody
	if dc.CollectHTTPBody(sentry.BodyOutgoingRequest) && hasRequestBody && request.ContentLength <= httputils.MaxBodyBytes {
		requestBody = httputils.NewLimitedBuffer(httputils.MaxBodyBytes)
		request.Body = &httputils.ReadCloser{
			Reader: io.TeeReader(request.Body, requestBody),
			Closer: request.Body,
		}
	}

	response, err := s.originalRoundTripper.RoundTrip(request)
	if requestBody != nil && !requestBody.Overflow() && requestBody.Len() > 0 {
		span.SetData("http.request.body", dc.FilterHTTPBody(requestBody.Bytes(), request.Header.Get("Content-Type")))
	}
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		return response, err
	}

	if response != nil {
		span.Status = sentry.HTTPtoSpanStatus(response.StatusCode)
		span.SetData("http.response.status_code", response.StatusCode)
		span.SetData("http.response_content_length", response.ContentLength)
		for key, value := range filterIncomingResponseHeaders(dc, response.Header) {
			span.SetData("http.response.header."+strings.ToLower(key), value)
		}
	}

	return response, err
}

func (s *SentryRoundTripper) shouldPropagate(requestURL string, clientTargets []string) bool {
	targets := make([]string, 0, len(clientTargets)+len(s.tracePropagationTargets))
	targets = append(targets, clientTargets...)
	targets = append(targets, s.tracePropagationTargets...)
	if len(targets) == 0 {
		return true
	}
	for _, target := range targets {
		if strings.Contains(requestURL, target) {
			return true
		}
	}
	return false
}

func addTraceHeaders(ctx context.Context, request *http.Request, propagateTraceparent bool) *http.Request {
	trace := sentry.GetTraceparent(ctx)
	baggage := sentry.GetBaggage(ctx)
	traceparent := ""
	if propagateTraceparent {
		traceparent = sentry.GetTraceparentW3C(ctx)
	}
	if trace == "" && baggage == "" && traceparent == "" {
		return request
	}

	request = request.Clone(request.Context())
	if trace != "" {
		request.Header.Set(sentry.SentryTraceHeader, trace)
	}
	if baggage != "" {
		existing := strings.Join(request.Header.Values(sentry.SentryBaggageHeader), ",")
		if merged, err := sentry.MergeBaggage(existing, baggage); err == nil {
			request.Header.Set(sentry.SentryBaggageHeader, merged)
		}
	}
	if traceparent != "" {
		request.Header.Set(sentry.TraceparentHeader, traceparent)
	}
	return request
}

func filterOutgoingRequestHeaders(dc sentry.DataCollection, headers http.Header) map[string]string {
	return dc.FilterRequestHeaders(headerStringMap(headers, dc, "Cookie", dc.FilterCookies))
}

func filterIncomingResponseHeaders(dc sentry.DataCollection, headers http.Header) map[string]string {
	return dc.FilterResponseHeaders(headerStringMap(headers, dc, "Set-Cookie", dc.FilterSetCookies))
}

// headerStringMap flattens HTTP headers into a map. Cookie headers are parsed
// and filtered through the cookie collection settings via filterCookies
// before header filtering.
func headerStringMap(headers http.Header, dc sentry.DataCollection, cookieHeader string, filterCookies func([]string) string) map[string]string {
	if len(headers) == 0 {
		return nil
	}

	m := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) == 0 {
			continue
		}

		value := strings.Join(values, ",")
		if strings.EqualFold(key, cookieHeader) {
			if !dc.CollectCookies() {
				continue
			}
			value = filterCookies(values)
			if value == "" {
				continue
			}
		}
		m[key] = value
	}
	return m
}
