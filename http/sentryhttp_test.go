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

		WantEvent *sentry.Event
	}{
		{
			Path: "/panic",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("test")
			}),

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
				Transaction: "GET /panic",
			},
		},
		{
			Path:   "/post",
			Method: "POST",
			Body:   "payload",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage("post: " + string(body))
			}),

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
				Transaction: "POST /post",
			},
		},
		{
			Path: "/get",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				hub.CaptureMessage("get")
			}),

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
				Transaction: "GET /get",
			},
		},
		{
			Path:   "/post/large",
			Method: "POST",
			Body:   largePayload,
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(body)/1024))
			}),

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
				Transaction: "POST /post/large",
			},
		},
		{
			Path:   "/post/body-ignored",
			Method: "POST",
			Body:   "client sends, server ignores, SDK doesn't read",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hub := sentry.GetHubFromContext(r.Context())
				hub.CaptureMessage("body ignored")
			}),

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
				Transaction: "POST /post/body-ignored",
			},
		},
	}

	eventsCh := make(chan *sentry.Event, len(tests))
	err := sentry.Init(sentry.ClientOptions{
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			eventsCh <- event
			return event
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
	for _, tt := range tests {
		wantRequest := tt.WantEvent.Request
		wantRequest.URL = srv.URL + wantRequest.URL
		wantRequest.Headers["Host"] = srv.Listener.Addr().String()
		want = append(want, tt.WantEvent)

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

	if ok := sentry.Flush(time.Second); !ok {
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
