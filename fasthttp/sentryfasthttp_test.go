package sentryfasthttp_test

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryfasthttp "github.com/getsentry/sentry-go/fasthttp"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB

	tests := []struct {
		Path    string
		Method  string
		Body    string
		Handler fasthttp.RequestHandler

		WantEvent *sentry.Event
	}{
		{
			Path: "/panic",
			Handler: func(*fasthttp.RequestCtx) {
				panic("test")
			},

			WantEvent: &sentry.Event{
				Level:   sentry.LevelFatal,
				Message: "test",
				Request: &sentry.Request{
					URL:    "http://example.com/panic",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
			},
		},
		{
			Path:   "/post",
			Method: "POST",
			Body:   "payload",
			Handler: func(ctx *fasthttp.RequestCtx) {
				hub := sentryfasthttp.GetHubFromContext(ctx)
				hub.CaptureMessage("post: " + string(ctx.Request.Body()))
			},

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: payload",
				Request: &sentry.Request{
					URL:    "http://example.com/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
			},
		},
		{
			Path: "/get",
			Handler: func(ctx *fasthttp.RequestCtx) {
				hub := sentryfasthttp.GetHubFromContext(ctx)
				hub.CaptureMessage("get")
			},

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "get",
				Request: &sentry.Request{
					URL:    "http://example.com/get",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
			},
		},
		{
			Path:   "/post/large",
			Method: "POST",
			Body:   largePayload,
			Handler: func(ctx *fasthttp.RequestCtx) {
				hub := sentryfasthttp.GetHubFromContext(ctx)
				hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(ctx.Request.Body())/1024))
			},

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: 15 KB",
				Request: &sentry.Request{
					URL:    "http://example.com/post/large",
					Method: "POST",
					// Actual request body omitted because too large.
					Data: "",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
			},
		},
		{
			Path:   "/post/body-ignored",
			Method: "POST",
			Body:   "client sends, fasthttp always reads, SDK reports",
			Handler: func(ctx *fasthttp.RequestCtx) {
				hub := sentryfasthttp.GetHubFromContext(ctx)
				hub.CaptureMessage("body ignored")
			},

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "body ignored",
				Request: &sentry.Request{
					URL:    "http://example.com/post/body-ignored",
					Method: "POST",
					// Actual request body included because fasthttp always
					// reads full request body.
					Data: "client sends, fasthttp always reads, SDK reports",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
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

	sentryHandler := sentryfasthttp.New(sentryfasthttp.Options{})
	ln := fasthttputil.NewInmemoryListener()
	handler := func(ctx *fasthttp.RequestCtx) {
		for _, tt := range tests {
			if string(ctx.Path()) == tt.Path {
				tt.Handler(ctx)
				return
			}
		}
		t.Errorf("Unhandled request: %#v", ctx)
	}
	done := make(chan struct{})
	go func() {
		if err := fasthttp.Serve(ln, sentryHandler.Handle(handler)); err != nil {
			t.Errorf("error in Serve: %s", err)
		}
		close(done)
	}()

	c := &fasthttp.Client{
		Dial: func(addr string) (net.Conn, error) {
			return ln.Dial()
		},
		ReadTimeout:  time.Second,
		WriteTimeout: time.Second,
	}

	var want []*sentry.Event
	for _, tt := range tests {
		want = append(want, tt.WantEvent)
		req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
		req.SetHost("example.com")
		req.URI().SetPath(tt.Path)
		req.Header.SetMethod(tt.Method)
		req.SetBodyString(tt.Body)
		if err := c.Do(req, res); err != nil {
			t.Fatalf("Request %q failed: %s", tt.Path, err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Errorf("Status code = %d", res.StatusCode())
		}
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
		cmpopts.IgnoreMapEntries(func(k string, v string) bool {
			// fasthttp changed Content-Length behavior in
			// https://github.com/valyala/fasthttp/commit/097fa05a697fc638624a14ab294f1336da9c29b0.
			// fasthttp changed Content-Type behavior in
			// https://github.com/valyala/fasthttp/commit/ffa0cabed8199819e372ebd2c739998914150ff2.
			// Since the specific values of those headers are not
			// important from the perspective of sentry-go, we
			// ignore them.
			return k == "Content-Length" || k == "Content-Type"
		}),
	}
	if diff := cmp.Diff(want, got, opts); diff != "" {
		t.Fatalf("Events mismatch (-want +got):\n%s", diff)
	}

	ln.Close()
	<-done
}
