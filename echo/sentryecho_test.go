package sentryecho_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/labstack/echo/v4"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB

	tests := []struct {
		RequestPath string
		RoutePath   string
		Method      string
		WantStatus  int
		Body        string
		Handler     echo.HandlerFunc

		WantEvent *sentry.Event
	}{
		{
			RequestPath: "/panic/1",
			RoutePath:   "/panic/:id",
			Method:      "GET",
			WantStatus:  200,
			Handler: func(c echo.Context) error {
				panic("test")
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
		{
			RequestPath: "/404/1",
			RoutePath:   "",
			Method:      "GET",
			WantStatus:  404,
			Handler:     nil,
			WantEvent:   nil,
		},
		{
			RequestPath: "/post",
			RoutePath:   "/post",
			Method:      "POST",
			WantStatus:  200,
			Body:        "payload",
			Handler: func(c echo.Context) error {
				hub := sentryecho.GetHubFromContext(c)
				body, err := io.ReadAll(c.Request().Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage("post: " + string(body))
				return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
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
			Handler: func(c echo.Context) error {
				hub := sentryecho.GetHubFromContext(c)
				hub.CaptureMessage("get")
				return c.JSON(http.StatusOK, map[string]string{"status": "get"})
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
			Handler: func(c echo.Context) error {
				hub := sentryecho.GetHubFromContext(c)
				body, err := io.ReadAll(c.Request().Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(body)/1024))
				return nil
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
			Handler: func(c echo.Context) error {
				hub := sentryecho.GetHubFromContext(c)
				hub.CaptureMessage("body ignored")
				return nil
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
			Handler: func(c echo.Context) error {
				return c.JSON(http.StatusBadRequest, map[string]string{"status": "bad_request"})
			},
			WantEvent: nil,
		},
	}

	eventsCh := make(chan *sentry.Event, len(tests))
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			eventsCh <- event
			return event
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	router := echo.New()
	router.Use(sentryecho.New(sentryecho.Options{}))

	for _, tt := range tests {
		switch tt.Method {
		case http.MethodGet:
			router.GET(tt.RoutePath, tt.Handler)
		case http.MethodPost:
			router.POST(tt.RoutePath, tt.Handler)
		}
	}

	srv := httptest.NewServer(router)
	defer srv.Close()

	c := srv.Client()
	c.Timeout = time.Second

	var want []*sentry.Event
	var wantCodes []sentry.SpanStatus
	for _, tt := range tests {
		if tt.WantEvent != nil && tt.WantEvent.Request != nil {
			wantRequest := tt.WantEvent.Request
			wantRequest.URL = srv.URL + wantRequest.URL
			wantRequest.Headers["Host"] = srv.Listener.Addr().String()
			want = append(want, tt.WantEvent)
		}
		wantCodes = append(wantCodes, sentry.HTTPtoSpanStatus(tt.WantStatus))

		req, err := http.NewRequest(tt.Method, srv.URL+tt.RequestPath, strings.NewReader(tt.Body))
		if err != nil {
			t.Fatal(err)
		}
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != tt.WantStatus {
			t.Errorf("Status code = %d expected: %d", res.StatusCode, tt.WantStatus)
		}
		err = res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
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

}
