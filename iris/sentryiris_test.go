package sentryiris_test

import (
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/kataras/iris/v12"
	"github.com/kataras/iris/v12/httptest"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB

	tests := []struct {
		RequestPath string
		RoutePath   string
		Method      string
		WantStatus  int
		Body        string
		Handler     iris.Handler

		WantEvent       *sentry.Event
		WantTransaction *sentry.Event
	}{
		{
			RequestPath: "/panic/1",
			RoutePath:   "/panic/{id}",
			Method:      "GET",
			WantStatus:  200,
			Handler: func(ctx iris.Context) {
				panic("test")
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /panic/{id}",
				Request: &sentry.Request{
					URL:    "/panic/1",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
			WantEvent: &sentry.Event{
				Level:   sentry.LevelFatal,
				Message: "test",
				Request: &sentry.Request{
					URL:    "/panic/1",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
		},
		// FIXME: Iris does not accept nil handler, therefore this 404 test case will be invalid.
		// {
		// 	RequestPath: "/404/1",
		// 	RoutePath:   "",
		// 	Method:      "GET",
		// 	WantStatus:  404,
		// 	Handler:     nil,
		// 	WantTransaction: &sentry.Event{
		// 		Level:       sentry.LevelInfo,
		// 		Type:        "transaction",
		// 		Transaction: "GET /404/1",
		// 		Request: &sentry.Request{
		// 			URL:    "/404/1",
		// 			Method: "GET",
		// 			Headers: map[string]string{
		// 				"Accept-Encoding": "gzip",
		// 				"User-Agent":      "Go-http-client/1.1",
		// 			},
		// 		},
		// 		TransactionInfo: &sentry.TransactionInfo{Source: "url"},
		// 		Extra:           map[string]interface{}{"http.request.method": string("GET")},
		// 	},
		// 	WantEvent: nil,
		// },
		{
			RequestPath: "/post",
			RoutePath:   "/post",
			Method:      "POST",
			WantStatus:  200,
			Body:        "payload",
			Handler: func(ctx iris.Context) {
				hub := sentryiris.GetHubFromContext(ctx)
				body, err := io.ReadAll(ctx.Request().Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage("post: " + string(body))
				ctx.StatusCode(http.StatusOK)
				_ = ctx.JSON(map[string]any{"status": "ok"})
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post",
				Request: &sentry.Request{
					URL:    "/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Content-Length":  "7",
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
		},
		{
			RequestPath: "/get",
			RoutePath:   "/get",
			Method:      "GET",
			WantStatus:  200,
			Handler: func(ctx iris.Context) {
				hub := sentryiris.GetHubFromContext(ctx)
				hub.CaptureMessage("get")
				ctx.StatusCode(http.StatusOK)
				_ = ctx.JSON(map[string]any{"status": "get"})
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /get",
				Request: &sentry.Request{
					URL:    "/get",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
		},
		{
			RequestPath: "/post/large",
			RoutePath:   "/post/large",
			Method:      "POST",
			WantStatus:  200,
			Body:        largePayload,
			Handler: func(ctx iris.Context) {
				hub := sentryiris.GetHubFromContext(ctx)
				body, err := io.ReadAll(ctx.Request().Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(body)/1024))
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/large",
				Request: &sentry.Request{
					URL:    "/post/large",
					Method: "POST",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  strconv.Itoa(len(largePayload)),
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
		},
		{
			RequestPath: "/post/body-ignored",
			RoutePath:   "/post/body-ignored",
			Method:      "POST",
			WantStatus:  200,
			Body:        "client sends, server ignores, SDK doesn't read",
			Handler: func(ctx iris.Context) {
				hub := sentryiris.GetHubFromContext(ctx)
				hub.CaptureMessage("body ignored")
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/body-ignored",
				Request: &sentry.Request{
					URL:    "/post/body-ignored",
					Method: "POST",
					// Actual request body omitted because not read.
					Data: "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  strconv.Itoa(len("client sends, server ignores, SDK doesn't read")),
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
		},
		{
			RequestPath: "/badreq",
			RoutePath:   "/badreq",
			Method:      "GET",
			WantStatus:  400,
			Handler: func(ctx iris.Context) {
				ctx.StatusCode(http.StatusBadRequest)
				_ = ctx.JSON(map[string]any{"status": "bad_request"})
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /badreq",
				Request: &sentry.Request{
					URL:    "/badreq",
					Method: "GET",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"User-Agent":      "Go-http-client/1.1",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
				Contexts: map[string]sentry.Context{
					"trace": sentry.TraceContext{
						Data: map[string]interface{}{
							"http.request.method":       http.MethodGet,
							"http.response.status_code": http.StatusBadRequest,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusInvalidArgument,
					}.Map(),
				},
			},
			WantEvent: nil,
		},
	}

	eventsCh := make(chan *sentry.Event, len(tests))
	transactionsCh := make(chan *sentry.Event, len(tests))
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			eventsCh <- event
			return event
		},
		BeforeSendTransaction: func(tx *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			transactionsCh <- tx
			return tx
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	router := iris.New()
	router.Use(sentryiris.New(sentryiris.Options{}))

	for _, tt := range tests {
		if tt.Handler == nil {
			t.Logf("Handler is nil for %s %s", tt.Method, tt.RequestPath)
			continue
		}

		router.Handle(tt.Method, tt.RoutePath, tt.Handler)
	}

	srv := httptest.New(t, router, httptest.URL("http://example.com"))

	var want []*sentry.Event
	var wanttrans []*sentry.Event
	var wantCodes []sentry.SpanStatus
	for _, tt := range tests {
		if tt.WantEvent != nil && tt.WantEvent.Request != nil {
			wantRequest := tt.WantEvent.Request
			wantRequest.URL = "http://example.com" + wantRequest.URL
			wantRequest.Headers["Host"] = "example.com"
			want = append(want, tt.WantEvent)
		}
		wantTransaction := tt.WantTransaction.Request
		wantTransaction.URL = "http://example.com" + wantTransaction.URL
		wantTransaction.Headers["Host"] = "example.com"
		wanttrans = append(wanttrans, tt.WantTransaction)
		wantCodes = append(wantCodes, sentry.HTTPtoSpanStatus(tt.WantStatus))

		reqHeaders := map[string]string{
			"Accept-Encoding": "gzip",
			"User-Agent":      "Go-http-client/1.1",
		}

		if len(tt.Body) > 0 {
			reqHeaders["Content-Length"] = strconv.Itoa(len(tt.Body))
		}

		res := srv.Request(tt.Method, tt.RequestPath).
			WithBytes([]byte(tt.Body)).
			WithHeaders(reqHeaders).
			Expect()

		res.Status(tt.WantStatus)
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
		cmpopts.IgnoreFields(
			sentry.Request{},
			"Env",
		),
		cmpopts.IgnoreMapEntries(func(k string, v any) bool {
			ignoredCtxEntries := []string{"span_id", "trace_id", "device", "os", "runtime"}
			for _, e := range ignoredCtxEntries {
				if k == e {
					return true
				}
			}
			return false
		}),
	}
	if diff := cmp.Diff(wanttrans, gott, optstrans); diff != "" {
		t.Fatalf("Transaction mismatch (-want +got):\n%s", diff)
	}

	if diff := cmp.Diff(wantCodes, statusCodes, cmp.Options{}); diff != "" {
		t.Fatalf("Transaction status codes mismatch (-want +got):\n%s", diff)
	}
}

func TestGetSpanFromContext(t *testing.T) {
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	router := iris.New()
	router.Get("/no-span", func(ctx iris.Context) {
		span := sentryiris.GetSpanFromContext(ctx)
		if span != nil {
			t.Error("expecting span to be nil")
		}

		ctx.StatusCode(http.StatusOK)
	})
	router.Use(sentryiris.New(sentryiris.Options{}))
	router.Get("/with-span", func(ctx iris.Context) {
		span := sentryiris.GetSpanFromContext(ctx)
		if span == nil {
			t.Error("expecting span to be not nil")
		}

		ctx.StatusCode(http.StatusOK)
	})

	tests := []struct{ RequestPath string }{
		{RequestPath: "/no-span"},
		{RequestPath: "/with-span"},
	}

	srv := httptest.New(t, router)

	for _, tt := range tests {
		res := srv.Request(http.MethodGet, tt.RequestPath).Expect()

		res.Status(http.StatusOK)

		if ok := sentry.Flush(testutils.FlushTimeout()); !ok {
			t.Fatal("sentry.Flush timed out")
		}
	}
}

func TestSetHubOnContext(t *testing.T) {
	app := iris.New()

	app.Get("/with-hub", func(ctx iris.Context) {
		hub := sentry.CurrentHub().Clone()
		sentryiris.SetHubOnContext(ctx, hub)

		newHub := sentryiris.GetHubFromContext(ctx)
		if newHub == nil {
			t.Error("expecting hub to be not nil")
		}

		if !reflect.DeepEqual(hub, newHub) {
			t.Error("expecting hub to be the same")
		}

		ctx.StatusCode(http.StatusOK)
	})

	srv := httptest.New(t, app)

	res := srv.Request(http.MethodGet, "/with-hub").Expect()

	res.Status(http.StatusOK)
}
