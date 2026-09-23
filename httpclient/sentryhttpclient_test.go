package sentryhttpclient_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	sentryhttpclient "github.com/getsentry/sentry-go/httpclient"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type captureRoundTripper struct {
	requests []*http.Request
}

func (c *captureRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, request)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

func contextWithClient(client *sentry.Client) context.Context {
	ctx, _ := sentry.WithIsolationScope(context.Background())
	return sentry.ContextWithClient(ctx, client)
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
				Description: "GET https://example.com/foo",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
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
				Description: "GET https://example.com:443/foo/bar?baz=123#readme",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
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
					"http.query":                   string("bar=123&abc=def"),
					"http.request.method":          string("HEAD"),
					"http.response.status_code":    int(400),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string("8443"),
				},
				Description: "HEAD https://example.com:8443/foo?bar=123&abc=def",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusInvalidArgument,
			},
		},
		{ //nolint:gosec // G101: not real credentials
			RequestMethod:      "POST",
			RequestURL:         "https://john:verysecurepassword@example.com:4321/secret",
			WantStatus:         200,
			WantResponseLength: 1024,
			WantSpan: &sentry.Span{ //nolint:gosec // G101: not real credentials
				Data: map[string]interface{}{
					"http.fragment":                     string(""),
					"http.query":                        string(""),
					"http.request.method":               string("POST"),
					"http.request.header.authorization": string("[Filtered]"),
					"http.response.status_code":         int(200),
					"http.response_content_length":      int64(1024),
					"server.address":                    string("example.com"),
					"server.port":                       string("4321"),
				},
				Description: "POST https://john:xxxxx@example.com:4321/secret",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
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
				Description: "POST https://example.com",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusInternalError,
			},
		},
		{
			RequestMethod: "OPTIONS",
			RequestURL:    "https://example.com",
			WantError:     false,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                string(""),
					"http.query":                   string(""),
					"http.request.method":          string("OPTIONS"),
					"http.response.status_code":    int(0),
					"http.response_content_length": int64(0),
					"server.address":               string("example.com"),
					"server.port":                  string(""),
				},
				Description: "OPTIONS https://example.com",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
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
				Description: "GET https://example.com/foo/bar?baz=123#readme",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
		},
		{
			RequestMethod:      "GET",
			RequestURL:         "https://example.net/foo/bar?baz=123#readme",
			TracerOptions:      []sentryhttpclient.SentryRoundTripTracerOption{sentryhttpclient.WithTracePropagationTargets([]string{"example.com"})},
			WantStatus:         200,
			WantResponseLength: 0,
			WantSpan: &sentry.Span{
				Data: map[string]interface{}{
					"http.fragment":                "readme",
					"http.query":                   "baz=123",
					"http.request.method":          "GET",
					"http.response.status_code":    200,
					"http.response_content_length": int64(0),
					"server.address":               "example.net",
					"server.port":                  "",
				},
				Description: "GET https://example.net/foo/bar?baz=123#readme",
				Op:          "http.client",
				Origin:      "manual",
				Sampled:     sentry.SampledTrue,
				Status:      sentry.SpanStatusOK,
			},
		},
	}

	spansCh := make(chan []*sentry.Span, len(tests))

	sentryClient, err := sentry.NewClient(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		ctx := contextWithClient(sentryClient)
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
			"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "contexts",
			"explicitSampled", "serializedTags", "serializedData", "serializationSafe",
		),
		testutils.EquateKeyValueStrings(),
	}
	for i, tt := range tests {
		gotSpans := got[i]

		if tt.WantSpan == nil {
			if len(gotSpans) > 0 {
				t.Errorf("Expected no span, got %+v", gotSpans)
			}
			continue
		}

		// The noopRoundTripper always returns a Content-Length response header,
		// which the default deny-list collects as span data.
		if !tt.WantError {
			tt.WantSpan.Data["http.response.header.content-length"] = strconv.Itoa(tt.WantResponseLength)
		}

		var foundMatch = false
		var diffs []string
		for _, gotSpan := range gotSpans {
			if diff := cmp.Diff(tt.WantSpan, gotSpan, optstrans); diff != "" {
				diffs = append(diffs, diff)
			} else {
				foundMatch = true
				break
			}
		}

		if !foundMatch {
			t.Errorf("Span mismatch (-want +got):\n%s", strings.Join(diffs, "\n"))
		}
	}
}

type bodyRoundTripper struct {
	responseBody    string
	responseHeaders map[string]string
}

func (b *bodyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		_, _ = io.Copy(io.Discard, request.Body)
		_ = request.Body.Close()
	}
	header := http.Header{}
	for k, v := range b.responseHeaders {
		header.Set(k, v)
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          io.NopCloser(strings.NewReader(b.responseBody)),
		ContentLength: int64(len(b.responseBody)),
		Request:       request,
	}, nil
}

func TestDataCollectionCollectsHeadersAndBodies(t *testing.T) {
	tests := []struct {
		name           string
		dataCollection *sentry.DataCollection
		wantData       map[string]interface{}
		wantAbsent     []string
	}{
		{
			name:           "default collects and filters headers and bodies",
			dataCollection: &sentry.DataCollection{},
			wantData: map[string]interface{}{
				"http.request.header.authorization": "[Filtered]",
				"http.request.header.cookie":        "session=[Filtered]; theme=dark",
				"http.request.header.x-custom":      "custom-value",
				"http.request.body":                 `{"password":"[Filtered]","safe":"keep"}`,
				"http.response.header.content-type": "application/json",
				"http.response.header.set-cookie":   "session=[Filtered]; theme=dark",
			},
			wantAbsent: []string{"http.response.body"},
		},
		{
			name: "bodies disabled keeps headers but drops bodies",
			dataCollection: &sentry.DataCollection{
				HTTPBodies: []sentry.BodyType{},
			},
			wantData: map[string]interface{}{
				"http.request.header.cookie":        "session=[Filtered]; theme=dark",
				"http.request.header.x-custom":      "custom-value",
				"http.response.header.content-type": "application/json",
				"http.response.header.set-cookie":   "session=[Filtered]; theme=dark",
			},
			wantAbsent: []string{"http.request.body", "http.response.body"},
		},
		{
			name: "response headers disabled keeps request headers and bodies",
			dataCollection: &sentry.DataCollection{
				HTTPHeaders: &sentry.HeaderCollectionConfig{
					Request:  &sentry.KeyValueCollectionBehavior{},
					Response: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
				},
			},
			wantData: map[string]interface{}{
				"http.request.header.authorization": "[Filtered]",
				"http.request.header.cookie":        "session=[Filtered]; theme=dark",
				"http.request.header.x-custom":      "custom-value",
				"http.request.body":                 `{"password":"[Filtered]","safe":"keep"}`,
			},
			wantAbsent: []string{
				"http.response.header.content-type",
				"http.response.body",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spansCh := make(chan []*sentry.Span, 1)
			sentryClient, err := sentry.NewClient(sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				DataCollection:   tt.dataCollection,
				BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					spansCh <- event.Spans
					return event
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			ctx := contextWithClient(sentryClient)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/api",
				strings.NewReader(`{"password":"secret","safe":"keep"}`))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer secret")
			request.Header.Set("Cookie", "session=secret; theme=dark")
			request.Header.Set("X-Custom", "custom-value")

			responseBody := `{"token":"abc","data":"ok"}`
			roundTripper := &bodyRoundTripper{
				responseBody: responseBody,
				responseHeaders: map[string]string{
					"Content-Type": "application/json",
					"Set-Cookie":   "session=secret; theme=dark",
				},
			}
			client := &http.Client{Transport: sentryhttpclient.NewSentryRoundTripper(roundTripper)}

			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}

			// The caller must still be able to read the full, untouched body.
			gotBody, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if string(gotBody) != responseBody {
				t.Errorf("response body = %q, want %q", gotBody, responseBody)
			}

			span.Finish()
			if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
				t.Fatal("sentry.Flush timed out")
			}
			close(spansCh)

			var got *sentry.Span
			for spans := range spansCh {
				for _, candidate := range spans {
					if candidate.Op == "http.client" {
						got = candidate
					}
				}
			}
			if got == nil {
				t.Fatal("missing http.client span")
			}

			for key, want := range tt.wantData {
				if diff := cmp.Diff(want, got.Data[key], testutils.EquateKeyValueStrings()); diff != "" {
					t.Errorf("span data[%q] mismatch (-want +got):\n%s", key, diff)
				}
			}
			for _, key := range tt.wantAbsent {
				if _, ok := got.Data[key]; ok {
					t.Errorf("span data[%q] should be absent, got %v", key, got.Data[key])
				}
			}
		})
	}
}

type headerRoundTripper struct {
	header http.Header
}

func (h *headerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     h.header.Clone(),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}, nil
}

func TestSetCookieResponseHeadersPreserveAttributes(t *testing.T) {
	spansCh := make(chan []*sentry.Span, 1)
	sentryClient, err := sentry.NewClient(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		DataCollection:   &sentry.DataCollection{},
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := contextWithClient(sentryClient)
	span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
	ctx = span.Context()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/api", nil)
	if err != nil {
		t.Fatal(err)
	}

	header := http.Header{}
	header.Add("Set-Cookie", "sessionid=abc123; Path=/; HttpOnly")
	header.Add("Set-Cookie", "theme=dark; Password=hunter2; Path=/settings; Secure")
	client := &http.Client{Transport: sentryhttpclient.NewSentryRoundTripper(&headerRoundTripper{header: header})}

	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()

	span.Finish()
	if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(spansCh)

	var got *sentry.Span
	for spans := range spansCh {
		for _, candidate := range spans {
			if candidate.Op == "http.client" {
				got = candidate
			}
		}
	}
	if got == nil {
		t.Fatal("missing http.client span")
	}

	want := "sessionid=[Filtered]; Path=/; HttpOnly, theme=dark; Password=[Filtered]; Path=/settings; Secure"
	if diff := cmp.Diff(want, got.Data["http.response.header.set-cookie"]); diff != "" {
		t.Errorf("span data[\"http.response.header.set-cookie\"] mismatch (-want +got):\n%s", diff)
	}
}

func TestIntegration_GlobalClientOptions(t *testing.T) {
	spansCh := make(chan []*sentry.Span, 1)

	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:           true,
		TracePropagationTargets: []string{"example.com"},
		PropagateTraceparent:    true,
		TracesSampleRate:        1.0,
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
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
	sentryTrace := response.Request.Header.Get(sentry.SentryTraceHeader)
	require.NotEmpty(t, sentryTrace)
	require.Equal(t, traceparentFromSentryTraceHeader(t, sentryTrace), response.Request.Header.Get(sentry.TraceparentHeader))
	require.Contains(t, response.Request.Header.Get(sentry.SentryBaggageHeader), "sentry-trace_id=")
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
			"mu", "parent", "sampleRate", "ctx", "dynamicSamplingContext", "recorder", "finishOnce", "contexts",
			"explicitSampled", "serializedTags", "serializedData", "serializationSafe",
		),
	}

	gotSpan := got[0]
	wantSpan := &sentry.Span{
		Data: map[string]interface{}{
			"http.fragment":                       string(""),
			"http.query":                          string(""),
			"http.request.method":                 string("POST"),
			"http.response.status_code":           int(200),
			"http.response_content_length":        int64(48),
			"http.response.header.content-length": string("48"),
			"server.address":                      string("example.com"),
			"server.port":                         string(""),
		},
		Description: "POST https://example.com",
		Op:          "http.client",
		Origin:      "manual",
		Sampled:     sentry.SampledTrue,
		Status:      sentry.SpanStatusOK,
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
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			spansCh <- event.Spans
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := contextWithClient(sentryClient)

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
	if value := response.Request.Header.Get("Baggage"); !strings.Contains(value, "sentry-trace_id=") {
		t.Errorf(`Expected scope trace baggage, got %s`, value)
	}

	if value := response.Request.Header.Get("Sentry-Trace"); value == "" {
		t.Errorf(`Expected "Sentry-Trace" header, got %s`, value)
	}
}

func TestIntegration_ResolvesPropagationOptionsFromRequestContext(t *testing.T) {
	t.Parallel()
	capture := &captureRoundTripper{}
	wrapper := sentryhttpclient.NewSentryRoundTripper(capture)
	for _, test := range []struct {
		name, target string
		propagate    bool
	}{
		{name: "matching", target: "example.com", propagate: true},
		{name: "nonmatching", target: "example.org"},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
				TracePropagationTargets: []string{test.target}, PropagateTraceparent: true, Release: "new",
			}))
			span := sentry.StartSpan(f.NewContext(context.Background()), "test")
			defer span.Finish()
			request, err := http.NewRequestWithContext(span.Context(), http.MethodGet, "https://example.com", nil)
			require.NoError(t, err)
			const existing = "othervendor=value,sentry-release=old"
			request.Header.Set(sentry.SentryBaggageHeader, existing)
			response, err := wrapper.RoundTrip(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			headers := capture.requests[len(capture.requests)-1].Header
			assert.Equal(t, test.propagate, headers.Get(sentry.SentryTraceHeader) != "")
			assert.Equal(t, test.propagate, headers.Get(sentry.TraceparentHeader) != "")
			baggage := headers.Get(sentry.SentryBaggageHeader)
			if test.propagate {
				assert.Contains(t, baggage, "othervendor=value")
				assert.Contains(t, baggage, "sentry-release=new")
				assert.NotContains(t, baggage, "sentry-release=old")
				assert.Equal(t, 1, strings.Count(baggage, "sentry-release="))
			} else {
				assert.Equal(t, existing, baggage)
			}
		})
	}
}

func TestPropagateTraceparentHeader(t *testing.T) {
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:         true,
		TracesSampleRate:      1.0,
		PropagateTraceparent:  true,
		BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event { return event },
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, _ := sentry.WithIsolationScope(context.Background())
	span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
	ctx = span.Context()

	request, err := http.NewRequestWithContext(ctx, "GET", "https://example.com/foo", nil)
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
	if response.Body != nil {
		response.Body.Close()
	}
	span.Finish()

	traceparent := response.Request.Header.Get("traceparent")
	if traceparent == "" {
		t.Fatalf(`Expected "traceparent" header to be set`)
	}

	sentryTrace := response.Request.Header.Get("Sentry-Trace")
	if sentryTrace == "" {
		t.Fatalf(`Expected "Sentry-Trace" header to be set`)
	}

	if want := traceparentFromSentryTraceHeader(t, sentryTrace); traceparent != want {
		t.Fatalf(`Unexpected "traceparent" header value, got %q want %q`, traceparent, want)
	}
}

func TestRoundTripDoesNotMutateCallerRequest(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name            string
		target          string
		nilHeader       bool
		wantHeaderCount int
	}{
		{name: "matching target", target: "example.com", wantHeaderCount: 1},
		{name: "matching target with nil header", target: "example.com", nilHeader: true, wantHeaderCount: 1},
		{name: "nonmatching target", target: "example.org"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sentrytest.Run(t, func(t *testing.T, fixture *sentrytest.Fixture) {
				transaction := sentry.StartTransaction(fixture.NewContext(context.Background()), "test")
				type requestContextKey struct{}
				requestCtx, cancel := context.WithCancel(context.WithValue(transaction.Context(), requestContextKey{}, "request value"))
				defer cancel()
				request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "https://example.com/foo", strings.NewReader(`{"key":"value"}`))
				require.NoError(t, err)
				if test.nilHeader {
					request.Header = nil
				}
				originalBody := request.Body
				originalHeader := request.Header.Clone()
				capture := &captureRoundTripper{}
				wrapped := sentryhttpclient.NewSentryRoundTripper(capture)

				for i := 0; i < 3; i++ {
					response, err := wrapped.RoundTrip(request)
					require.NoError(t, err)
					response.Body.Close()
				}

				assert.Same(t, requestCtx, request.Context(), "caller request context was replaced")
				if request.Body != originalBody {
					t.Fatal("caller request body was replaced")
				}
				assert.Equal(t, originalHeader, request.Header, "caller request headers changed")
				require.Len(t, capture.requests, 3)
				for i, roundTripRequest := range capture.requests {
					assert.NotSame(t, request, roundTripRequest, "round trip %d reused the caller request", i)
					for _, header := range []string{sentry.SentryTraceHeader, sentry.SentryBaggageHeader, sentry.TraceparentHeader} {
						assert.Len(t, roundTripRequest.Header.Values(header), test.wantHeaderCount, "round trip %d header %s", i, header)
					}
					child := sentry.SpanFromContext(roundTripRequest.Context())
					require.NotNil(t, child)
					assert.Equal(t, "http.client", child.Op)
					assert.Equal(t, transaction.SpanID, child.ParentSpanID)
					assert.Equal(t, "request value", roundTripRequest.Context().Value(requestContextKey{}))
				}
				cancel()
				for i, roundTripRequest := range capture.requests {
					select {
					case <-roundTripRequest.Context().Done():
					default:
						t.Fatalf("round trip %d context did not preserve cancellation", i)
					}
				}
				transaction.Finish()
				fixture.Flush()
				events := fixture.Events()
				require.Len(t, events, 1)
				assert.Equal(t, "transaction", events[0].Type)
				require.Len(t, events[0].Spans, 3)
				for _, span := range events[0].Spans {
					assert.Equal(t, "http.client", span.Op)
				}
			}, sentrytest.WithClientOptions(sentry.ClientOptions{
				EnableTracing:           true,
				TracesSampleRate:        1.0,
				TracePropagationTargets: []string{test.target},
				PropagateTraceparent:    true,
			}))
		})
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

		if res != nil && res.Body != nil {
			res.Body.Close()
		}
	})
}

func traceparentFromSentryTraceHeader(t *testing.T, sentryTrace string) string {
	t.Helper()

	traceParentContext, valid := sentry.ParseTraceParentContext([]byte(sentryTrace))
	if !valid {
		t.Fatalf("Invalid sentry-trace header: %q", sentryTrace)
	}

	traceFlags := "00"
	if traceParentContext.Sampled == sentry.SampledTrue {
		traceFlags = "01"
	}

	return fmt.Sprintf("00-%s-%s-%s", traceParentContext.TraceID.String(), traceParentContext.ParentSpanID.String(), traceFlags)
}

func TestDataCollectionFiltersQuerySpanData(t *testing.T) {
	tests := []struct {
		name           string
		dataCollection *sentry.DataCollection
		requestURL     string
		wantData       map[string]interface{}
		wantDesc       string
	}{
		{
			name:       "filters sensitive query values",
			requestURL: "https://example.com/foo?page=1&token=secret#readme",
			wantData: map[string]interface{}{
				"http.fragment":                       "readme",
				"http.query":                          "page=1&token=[Filtered]",
				"http.request.method":                 "GET",
				"http.response.status_code":           200,
				"http.response_content_length":        int64(0),
				"http.response.header.content-length": "0",
				"server.address":                      "example.com",
				"server.port":                         "",
			},
			wantDesc: "GET https://example.com/foo?page=1&token=[Filtered]#readme",
		},
		{
			name: "omits query values when disabled",
			dataCollection: &sentry.DataCollection{
				QueryParams: &sentry.KeyValueCollectionBehavior{Mode: sentry.CollectionOff},
			},
			requestURL: "https://example.com/foo?page=1&token=secret#readme",
			wantData: map[string]interface{}{
				"http.fragment":                       "readme",
				"http.request.method":                 "GET",
				"http.response.status_code":           200,
				"http.response_content_length":        int64(0),
				"http.response.header.content-length": "0",
				"server.address":                      "example.com",
				"server.port":                         "",
			},
			wantDesc: "GET https://example.com/foo#readme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spansCh := make(chan []*sentry.Span, 1)
			sentryClient, err := sentry.NewClient(sentry.ClientOptions{
				EnableTracing:    true,
				TracesSampleRate: 1.0,
				DataCollection:   tt.dataCollection,
				BeforeSendTransaction: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					spansCh <- event.Spans
					return event
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			ctx := contextWithClient(sentryClient)
			span := sentry.StartSpan(ctx, "fake_parent", sentry.WithTransactionName("Fake Parent"))
			ctx = span.Context()

			request, err := http.NewRequestWithContext(ctx, http.MethodGet, tt.requestURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: sentryhttpclient.NewSentryRoundTripper(&noopRoundTripper{ExpectResponseStatus: http.StatusOK})}
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			span.Finish()

			if ok := sentryClient.Flush(testutils.FlushTimeout()); !ok {
				t.Fatal("sentry.Flush timed out")
			}
			close(spansCh)

			var got *sentry.Span
			for spans := range spansCh {
				for _, candidate := range spans {
					if candidate.Op == "http.client" {
						got = candidate
						break
					}
				}
			}
			if got == nil {
				t.Fatal("missing http.client span")
			}
			if diff := cmp.Diff(tt.wantData, got.Data, testutils.EquateKeyValueStrings()); diff != "" {
				t.Fatalf("span data mismatch (-want +got):\n%s", diff)
			}
			if diff := cmp.Diff(tt.wantDesc, got.Description, testutils.EquateKeyValueStrings()); diff != "" {
				t.Fatalf("span description mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFrozenEmptyBaggageReplacesPreviousTrace(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		existing []string
		want     string
	}{
		{name: "third-party members", existing: []string{"othervendor=value", "sentry-release=old,sentry-trace_id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa,sentry-extra=stale"}, want: "othervendor=value"},
		{name: "malformed baggage", existing: []string{"not-valid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := sentrytest.NewFixture(t)
			ctx := f.NewContext(context.Background())
			sentry.StartTransaction(ctx, "incoming", sentry.ContinueTrace("11111111111111111111111111111111-2222222222222222-1", "")).Finish()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
			require.NoError(t, err)
			for _, value := range test.existing {
				request.Header.Add(sentry.SentryBaggageHeader, value)
			}
			original := request.Header.Clone()
			capture := &captureRoundTripper{}
			response, err := sentryhttpclient.NewSentryRoundTripper(capture).RoundTrip(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Len(t, capture.requests, 1)
			forwarded := capture.requests[0]
			require.Nil(t, sentry.SpanFromContext(forwarded.Context()))
			require.Equal(t, sentry.GetTraceparent(ctx), forwarded.Header.Get(sentry.SentryTraceHeader))
			require.Equal(t, test.want, forwarded.Header.Get(sentry.SentryBaggageHeader))
			require.Equal(t, original, request.Header)
		})
	}
}

type alternatingTraceIntegration struct {
	lookups                   int
	selectedTrace, otherTrace sentry.TraceID
	selectedSpan, otherSpan   sentry.SpanID
}

func (*alternatingTraceIntegration) Name() string             { return "alternating-trace" }
func (*alternatingTraceIntegration) SetupOnce(*sentry.Client) {}
func (integration *alternatingTraceIntegration) ResolveTraceContext(context.Context) (sentry.TraceID, sentry.SpanID, sentry.Sampled, bool) {
	integration.lookups++
	if integration.lookups == 1 {
		return integration.selectedTrace, integration.selectedSpan, sentry.SampledTrue, true
	}
	return integration.otherTrace, integration.otherSpan, sentry.SampledFalse, true
}

func TestPropagationHeadersUseTheSameTraceDecision(t *testing.T) {
	t.Parallel()
	selectedTrace, selectedSpan := sentry.TraceID{1}, sentry.SpanID{2}
	otherTrace, otherSpan := sentry.TraceID{3}, sentry.SpanID{4}
	resolver := &alternatingTraceIntegration{selectedTrace: selectedTrace, selectedSpan: selectedSpan, otherTrace: otherTrace, otherSpan: otherSpan}
	f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{
		PropagateTraceparent: true,
		Integrations:         func(in []sentry.Integration) []sentry.Integration { return append(in, resolver) },
	}))
	ctx := f.NewContext(context.Background())
	root := sentry.StartTransaction(ctx, "native")
	defer root.Finish()
	resolver.lookups = 0
	sentry.ScopeFromContext(ctx).SetPropagationContext(sentry.PropagationContext{
		TraceID: otherTrace, SpanID: otherSpan,
		DynamicSamplingContext: sentry.DynamicSamplingContext{Frozen: true, Entries: map[string]string{"trace_id": otherTrace.String(), "release": "foreign"}},
	})
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	request.Header.Set(sentry.SentryBaggageHeader, "othervendor=value,sentry-release=stale")
	capture := &captureRoundTripper{}
	response, err := sentryhttpclient.NewSentryRoundTripper(capture).RoundTrip(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	forwarded := capture.requests[0].Header
	require.Equal(t, fmt.Sprintf("%s-%s-1", selectedTrace, selectedSpan), forwarded.Get(sentry.SentryTraceHeader))
	require.Equal(t, fmt.Sprintf("00-%s-%s-01", selectedTrace, selectedSpan), forwarded.Get(sentry.TraceparentHeader))
	require.Equal(t, "othervendor=value", forwarded.Get(sentry.SentryBaggageHeader))
}

func TestTracePropagationTargets(t *testing.T) {
	t.Parallel()

	for _, clientTargets := range []bool{false, true} {
		for _, parentSpan := range []bool{false, true} {
			for _, propagateTraceparent := range []bool{false, true} {
				for _, tt := range []struct {
					name            string
					targets         []string
					otherTargets    []string
					wantPropagation bool
				}{
					{name: "default", wantPropagation: true},
					{name: "empty", targets: []string{}},
					{name: "matching", targets: []string{"other.example", "example.com"}, wantPropagation: true},
					{name: "nonmatching", targets: []string{"internal.service.local"}},
					{name: "empty with matching other", targets: []string{}, otherTargets: []string{"example.com"}, wantPropagation: true},
					{name: "nonmatching with matching other", targets: []string{"internal.service.local"}, otherTargets: []string{"example.com"}, wantPropagation: true},
					{name: "matching with empty other", targets: []string{"example.com"}, otherTargets: []string{}, wantPropagation: true},
				} {
					t.Run(fmt.Sprintf("%s/client=%t/parent=%t/traceparent=%t", tt.name, clientTargets, parentSpan, propagateTraceparent), func(t *testing.T) {
						opts := sentry.ClientOptions{
							EnableTracing:        true,
							TracesSampleRate:     1,
							PropagateTraceparent: propagateTraceparent,
						}
						var tracerOptions []sentryhttpclient.SentryRoundTripTracerOption
						if clientTargets {
							opts.TracePropagationTargets = tt.targets
							if tt.otherTargets != nil {
								tracerOptions = append(tracerOptions, sentryhttpclient.WithTracePropagationTargets(tt.otherTargets))
							}
						} else {
							tracerOptions = append(tracerOptions, sentryhttpclient.WithTracePropagationTargets(tt.targets))
							opts.TracePropagationTargets = tt.otherTargets
						}
						fixture := sentrytest.NewFixture(t, sentrytest.WithClientOptions(opts))
						ctx := fixture.NewContext(context.Background())
						var transaction *sentry.Span
						if parentSpan {
							transaction = sentry.StartTransaction(ctx, "outgoing request")
							ctx = transaction.Context()
						}
						request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com/foo", nil)
						require.NoError(t, err)
						transport := &captureRoundTripper{}
						client := &http.Client{Transport: sentryhttpclient.NewSentryRoundTripper(transport, tracerOptions...)}
						response, err := client.Do(request)
						require.NoError(t, err)
						require.NoError(t, response.Body.Close())
						require.Len(t, transport.requests, 1)
						headers := transport.requests[0].Header
						assert.Equal(t, tt.wantPropagation, headers.Get(sentry.SentryTraceHeader) != "")
						assert.Equal(t, tt.wantPropagation, len(headers.Values(sentry.SentryBaggageHeader)) != 0)
						assert.Equal(t, tt.wantPropagation && propagateTraceparent, headers.Get(sentry.TraceparentHeader) != "")
						assert.Empty(t, request.Header)

						if transaction != nil {
							transaction.Finish()
						}
						fixture.Flush()
						events := fixture.Events()
						if !parentSpan {
							assert.Empty(t, events)
							return
						}
						require.Len(t, events, 1)
						assert.Equal(t, "transaction", events[0].Type)
						require.Len(t, events[0].Spans, 1)
						span := events[0].Spans[0]
						assert.Equal(t, "http.client", span.Op)
						assert.Equal(t, "GET https://example.com/foo", span.Description)
						assert.Equal(t, transaction.SpanID, span.ParentSpanID)
						assert.Equal(t, sentry.SpanStatusOK, span.Status)
						assert.Equal(t, float64(http.StatusOK), span.Data["http.response.status_code"])
						assert.False(t, span.EndTime.IsZero())
					})
				}
			}
		}
	}
}
