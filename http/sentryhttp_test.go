package sentryhttp_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB

	tests := []struct {
		Path    string
		Method  string
		Body    string
		Handler http.Handler

		WantStatus      int
		WantEvent       *sentry.Event
		WantTransaction *sentry.Event
	}{
		{
			Path: "/panic",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("test")
			}),

			WantStatus: http.StatusOK,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelFatal,
				Message: "test",
				Request: &sentry.Request{
					URL:    "/panic",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /panic",
				Request: &sentry.Request{
					URL:    "/panic",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "url"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodGet,
							"http.response.status_code": http.StatusOK,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusOK,
					}.Map(),
				},
			},
		},
		{
			Path:   "/post",
			Method: "POST",
			Body:   "payload",
			Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage("post: " + string(body))
			}),

			WantStatus: http.StatusOK,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: payload",
				Request: &sentry.Request{
					URL:    "/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "7",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Transaction: "POST /post",
				Type:        "transaction",
				Request: &sentry.Request{
					URL:    "/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "7",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "url"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodPost,
							"http.response.status_code": http.StatusOK,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusOK,
					}.Map(),
				},
			},
		},
		{
			Path: "/get",
			Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				hub.CaptureMessage("get")
			}),

			WantStatus: http.StatusOK,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "get",
				Request: &sentry.Request{
					URL:    "/get",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Transaction: "GET /get",
				Type:        "transaction",
				Request: &sentry.Request{
					URL:    "/get",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "url"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodGet,
							"http.response.status_code": http.StatusOK,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusOK,
					}.Map(),
				},
			},
		},
		{
			Path:   "/post/large",
			Method: "POST",
			Body:   largePayload,
			Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(body)/1024))
			}),

			WantStatus: http.StatusOK,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: 15 KB",
				Request: &sentry.Request{
					URL:    "/post/large",
					Method: "POST",
					// Actual request body omitted because too large.
					Data: "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "15360",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Transaction: "POST /post/large",
				Type:        "transaction",
				Request: &sentry.Request{
					URL:    "/post/large",
					Method: "POST",
					// Actual request body omitted because too large.
					Data: "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "15360",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "url"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodPost,
							"http.response.status_code": http.StatusOK,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusOK,
					}.Map(),
				},
			},
		},
		{
			Path:   "/post/body-ignored",
			Method: "POST",
			Body:   "client sends, server ignores, SDK doesn't read",
			Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				hub.CaptureMessage("body ignored")
			}),

			WantStatus: http.StatusOK,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "body ignored",
				Request: &sentry.Request{
					URL:    "/post/body-ignored",
					Method: "POST",
					// Actual request body omitted because not read.
					Data: "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "46",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Transaction: "POST /post/body-ignored",
				Type:        "transaction",
				Request: &sentry.Request{
					URL:    "/post/body-ignored",
					Method: "POST",
					// Actual request body omitted because not read.
					Data: "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  "46",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "url"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodPost,
							"http.response.status_code": http.StatusOK,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusOK,
					}.Map(),
				}},
		},
	}

	eventsCh := make(chan *sentry.Event, len(tests))
	transactionsCh := make(chan *sentry.Event, len(tests))
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			eventsCh <- event
			return event
		},
		BeforeSendTransaction: func(tx *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			transactionsCh <- tx
			return tx
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	sentryHandler := sentryhttp.New(sentryhttp.Options{})
	handler := func(w http.ResponseWriter, r *http.Request) {
		for _, tt := range tests {
			if r.URL.Path == tt.Path {
				tt.Handler.ServeHTTP(w, r)
				return
			}
		}
		t.Errorf("Unhandled request: %#v", r)
	}
	srv := httptest.NewServer(sentryHandler.HandleFunc(handler))
	defer srv.Close()

	c := srv.Client()
	c.Timeout = time.Second

	var want []*sentry.Event
	var wantTrans []*sentry.Event
	var wantCodes []sentry.SpanStatus

	for _, tt := range tests {
		wantRequest := tt.WantEvent.Request
		wantRequest.URL = srv.URL + wantRequest.URL
		wantRequest.Headers["Host"] = srv.Listener.Addr().String()
		want = append(want, tt.WantEvent)

		wantTransaction := tt.WantTransaction.Request
		wantTransaction.URL = srv.URL + wantTransaction.URL
		wantTransaction.Headers["Host"] = srv.Listener.Addr().String()
		wantTrans = append(wantTrans, tt.WantTransaction)
		wantCodes = append(wantCodes, sentry.HTTPtoSpanStatus(tt.WantStatus))

		req, err := http.NewRequest(tt.Method, srv.URL+tt.Path, strings.NewReader(tt.Body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusOK {
			t.Errorf("Status code = %d", res.StatusCode)
		}
		res.Body.Close()
	}

	if ok := sentry.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(eventsCh)
	var got []*sentry.Event
	for e := range eventsCh {
		got = append(got, e)
	}
	opts := cmp.Options{
		cmpopts.IgnoreFields(
			sentry.Event{},
			"Contexts", "EventID", "Extra", "Platform", "Modules",
			"Release", "Sdk", "ServerName", "Tags", "Timestamp",
			"sdkMetaData",
		),
		cmpopts.IgnoreFields(
			sentry.Request{},
			"Env",
		),
	}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Fatalf("Events mismatch (-want +got):\n%s", diff)
	}

	close(transactionsCh)
	var gott []*sentry.Event
	var statusCodes []sentry.SpanStatus
	for e := range transactionsCh {
		gott = append(gott, e)
		statusCodes = append(statusCodes, e.Contexts["trace"]["status"].(sentry.SpanStatus))
	}

	optstrans := cmp.Options{
		cmpopts.IgnoreFields(
			sentry.Event{},
			"EventID", "Platform", "Modules",
			"Release", "Sdk", "ServerName", "Timestamp",
			"sdkMetaData", "StartTime", "Spans",
		),
		cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
			ignoredCtxEntries := []string{"span_id", "trace_id", "device", "os", "runtime"}
			for _, e := range ignoredCtxEntries {
				if k == e {
					return true
				}
			}
			return false
		}),
	}
	if diff := cmp.Diff(wantTrans, gott, optstrans); diff != "" {
		t.Fatalf("Transaction mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantCodes, statusCodes, cmp.Options{}); diff != "" {
		t.Fatalf("Transaction status codes mismatch (-want +got):\n%s", diff)
	}
}
