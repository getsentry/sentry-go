package sentryhttpclient_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type noopRoundTripper struct {
	ExpectResponseStatus int
	ExpectResponseLength int
	ExpectError          bool
}

func (n *noopRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if n.ExpectError {
		return nil, errors.New("error")
	}

	responseBody := make([]byte, n.ExpectResponseLength)
	_, _ = rand.Read(responseBody)
	return &http.Response{
		Status:     "",
		StatusCode: n.ExpectResponseStatus,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header: map[string][]string{
			"Content-Length": {strconv.Itoa(len(responseBody))},
		},
		Body:             io.NopCloser(bytes.NewReader(responseBody)),
		ContentLength:    int64(len(responseBody)),
		TransferEncoding: []string{},
		Close:            false,
		Uncompressed:     false,
		Trailer:          map[string][]string{},
		Request:          request,
		TLS:              &tls.ConnectionState{},
	}, nil
}

func TestIntegration(t *testing.T) {
	tests := []struct {
		RequestMethod      string
		RequestURL         string
		TracerOptions      []sentryhttpclient.SentryRoundTripTracerOption
		WantStatus         int
		WantResponseLength int
		WantError          bool
		WantSpan           *sentry.Span
	}{
		{
			RequestMethod:      "GET",
			RequestURL:         "https://example.com/foo",
			WantStatus:         200,
			WantResponseLength: 0,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string(""),
					"http.query":                   string(""),
					"http.request.method":          string("GET"),
					"http.response.status_code":    int(200),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string(""),
				},
				Name:    "GET https://example.com/foo",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusOK,
			},
		},
		{
			RequestMethod:      "GET",
			RequestURL:         "https://example.com:443/foo/bar?baz=123#readme",
			TracerOptions:      []sentryhttpclient.SentryRoundTripTracerOption{nil, nil, nil},
			WantStatus:         200,
			WantResponseLength: 0,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string("readme"),
					"http.query":                   string("baz=123"),
					"http.request.method":          string("GET"),
					"http.response.status_code":    int(200),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string("443"),
				},
				Name:    "GET https://example.com:443/foo/bar?baz=123#readme",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusOK,
			},
		},
		{
			RequestMethod:      "HEAD",
			RequestURL:         "https://example.com:8443/foo?bar=123&abc=def",
			TracerOptions:      []sentryhttpclient.SentryRoundTripTracerOption{},
			WantStatus:         400,
			WantResponseLength: 0,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string(""),
					"http.query":                   string("abc=def&bar=123"),
					"http.request.method":          string("HEAD"),
					"http.response.status_code":    int(400),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string("8443"),
				},

				Name:    "HEAD https://example.com:8443/foo?bar=123&abc=def",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusInvalidArgument,
			},
		},
		{
			RequestMethod:      "POST",
			RequestURL:         "https://john:verysecurepassword@example.com:4321/secret",
			WantStatus:         200,
			WantResponseLength: 1024,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string(""),
					"http.query":                   string(""),
					"http.request.method":          string("POST"),
					"http.response.status_code":    int(200),
					"http.response_content_length": int64(1024),
					"server.address":               string("example.com"),
					"server.port":                  string("4321"),
				},
				Name:    "POST https://john:xxxxx@example.com:4321/secret",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusOK,
			},
		},
		{
			RequestMethod: "POST",
			RequestURL:    "https://example.com",
			WantError:     true,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":       string(""),
					"http.query":          string(""),
					"http.request.method": string("POST"),
					"server.address":      string("example.com"),
					"server.port":         string(""),
				},
				Name:    "POST https://example.com",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusInternalError,
			},
		},
		{
			RequestMethod: "OPTIONS",
			RequestURL:    "https://example.com",
			WantError:     false,
			WantSpan:      nil,
		},
		{
			RequestMethod:      "GET",
			RequestURL:         "https://example.com/foo/bar?baz=123#readme",
			TracerOptions:      []sentryhttpclient.SentryRoundTripTracerOption{sentryhttpclient.WithTracePropagationTargets([]string{"example.com"}), sentryhttpclient.WithTracePropagationTargets([]string{"example.org"})},
			WantStatus:         200,
			WantResponseLength: 0,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string("readme"),
					"http.query":                   string("baz=123"),
					"http.request.method":          string("GET"),
					"http.response.status_code":    int(200),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string(""),
				},
				Name:    "GET https://example.com/foo/bar?baz=123#readme",
				Op:      "http.client",
				Origin:  "manual",
				Sampled: sentry.SampledTrue,
				Status:  sentry.SpanStatusOK,
			},
		},
		{
			RequestMethod:      "GET",
			RequestURL:         "https://example.net/foo/bar?baz=123#readme",
			TracerOptions:      []sentryhttpclient.SentryRoundTripTracerOption{sentryhttpclient.WithTracePropagationTargets([]string{"example.com"})},
			WantStatus:         200,
			WantResponseLength: 0,
			WantSpan:           nil,
		},
	}

	spansCh := make(chan []*sentry.Span, len(tests))

	sentryClient, err := sentry.NewClient(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		hub := sentry.NewHub(sentryClient, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
		ctx = span.Context()

		request, err := http.NewRequestWithContext(ctx, tt.RequestMethod, tt.RequestURL, nil)
		if err != nil && !tt.WantError {
			t.Fatal(err)
		}

		roundTripper := &noopRoundTripper{
			ExpectResponseStatus: tt.WantStatus,
			ExpectResponseLength: tt.WantResponseLength,
			ExpectError:          tt.WantError,
		}

		client := &http.Client{
			Transport: sentryhttpclient.NewSentryRoundTripper(roundTripper, tt.TracerOptions...),
		}

		response, err := client.Do(request)
		if err != nil && !tt.WantError {
			t.Fatal(err)
		}

		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		span.Finish()
	}

	if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(spansCh)

	var got [][]*sentry.Span
	for e := range spansCh {
		got = append(got, e)
	}

	optstrans := cmp.Options{
		cmpopts.IgnoreFields(
			sentry.Span{},
			"TraceID", "SpanID", "ParentSpanID", "StartTime", "EndTime",
			"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "collectProfile", "contexts",
		),
	}
	for i, tt := range tests {
		var foundMatch = false
		gotSpans := got[i]

		var diffs []string
		for _, gotSpan := range gotSpans {
			if diff := cmp.Diff(tt.WantSpan, gotSpan, optstrans); diff != "" {
				diffs = append(diffs, diff)
			} else {
				foundMatch = true
				break
			}
		}

		if tt.WantSpan != nil && !foundMatch {
			t.Errorf("Span mismatch (-want +got):\n%s", strings.Join(diffs, "\n"))
		} else if tt.WantSpan == nil && foundMatch {
			t.Errorf("Expected no span, got %+v", gotSpans)
		}
	}
}

func TestIntegration_GlobalClientOptions(t *testing.T) {
	spansCh := make(chan []*sentry.Span, 1)

	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:           true,
		TracePropagationTargets: []string{"example.com"},
		TracesSampleRate:        1.0,
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := sentry.SetHubOnContext(context.Background(), sentry.CurrentHub())
	span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
	ctx = span.Context()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	roundTripper := &noopRoundTripper{
		ExpectResponseStatus: 200,
		ExpectResponseLength: 48,
		ExpectError:          false,
	}

	client := &http.Client{
		Transport: sentryhttpclient.NewSentryRoundTripper(roundTripper),
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	span.Finish()

	if ok := sentry.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(spansCh)

	var got []*sentry.Span
	for e := range spansCh {
		got = append(got, e...)
	}

	optstrans := cmp.Options{
		cmpopts.IgnoreFields(
			sentry.Span{},
			"TraceID", "SpanID", "ParentSpanID", "StartTime", "EndTime",
			"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "collectProfile", "contexts",
		),
	}

	gotSpan := got[0]
	wantSpan := &sentry.Span{
		Data: map[string]interface{}{
			"http.fragment":                string(""),
			"http.query":                   string(""),
			"http.request.method":          string("POST"),
			"http.response.status_code":    int(200),
			"http.response_content_length": int64(48),
			"server.address":               string("example.com"),
			"server.port":                  string(""),
		},
		Name:    "POST https://example.com",
		Op:      "http.client",
		Origin:  "manual",
		Sampled: sentry.SampledTrue,
		Status:  sentry.SpanStatusOK,
	}

	if diff := cmp.Diff(wantSpan, gotSpan, optstrans); diff != "" {
		t.Errorf("Span mismatch (-want +got):\n%s", diff)
	}
}

func TestIntegration_NoParentSpan(t *testing.T) {
	spansCh := make(chan []*sentry.Span, 1)

	sentryClient, err := sentry.NewClient(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSendTransaction: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	hub := sentry.NewHub(sentryClient, sentry.NewScope())
	ctx := sentry.SetHubOnContext(context.Background(), hub)

	request, err := http.NewRequestWithContext(ctx, "GET", "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}

	roundTripper := &noopRoundTripper{
		ExpectResponseStatus: 200,
		ExpectResponseLength: 0,
	}

	client := &http.Client{
		Transport: sentryhttpclient.NewSentryRoundTripper(roundTripper),
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}

	response.Body.Close()

	if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(spansCh)

	var got [][]*sentry.Span
	for e := range spansCh {
		got = append(got, e)
	}

	// Expect no spans.
	if len(got) != 0 {
		t.Errorf("Expected no spans, got %d", len(got))
	}

	// Expect "Baggage" and "Sentry-Trace" headers.
	if value := response.Request.Header.Get("Baggage"); value != "" {
		t.Errorf(`Expected "Baggage" header to be empty, got %s`, value)
	}

	if value := response.Request.Header.Get("Sentry-Trace"); value == "" {
		t.Errorf(`Expected "Sentry-Trace" header, got %s`, value)
	}
}

func TestDefaults(t *testing.T) {
	t.Run("Create a regular outgoing HTTP request with default NewSentryRoundTripper", func(t *testing.T) {
		roundTripper := sentryhttpclient.NewSentryRoundTripper(nil)
		client := &http.Client{Transport: roundTripper}

		res, err := client.Head("https://sentry.io")
		if err != nil {
			t.Error(err)
		}

		if res.Body != nil {
			res.Body.Close()
		}
	})

	t.Run("Create a regular outgoing HTTP request with default SentryHttpClient", func(t *testing.T) {
		client := sentryhttpclient.SentryHTTPClient

		res, err := client.Head("https://sentry.io")
		if err != nil {
			t.Error(err)
		}

		if res.Body != nil {
			res.Body.Close()
		}
	})
}
