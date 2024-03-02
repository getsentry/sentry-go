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
	"github.com/getsentry/sentry-go/internal/testutils"
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

		WantEvent       *sentry.Event
		WantTransaction *sentry.Event
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /panic",
				Request: &sentry.Request{
					URL:    "http://example.com/panic",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post",
				Request: &sentry.Request{
					URL:    "http://example.com/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /get",
				Request: &sentry.Request{
					URL:    "http://example.com/get",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/large",
				Request: &sentry.Request{
					URL:    "http://example.com/post/large",
					Method: "POST",
					Data:   "",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/body-ignored",
				Request: &sentry.Request{
					URL:    "http://example.com/post/body-ignored",
					Method: "POST",
					Data:   "client sends, fasthttp always reads, SDK reports",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fasthttp",
					},
				},
				TransactionInfo: &sentry.TransactionInfo{Source: "route"},
			},
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

	var wantEvents []*sentry.Event
	var wantTransactions []*sentry.Event
	for _, tt := range tests {
		wantEvents = append(wantEvents, tt.WantEvent)
		wantTransactions = append(wantTransactions, tt.WantTransaction)
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

	if ok := sentry.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("sentry.Flush timed out")
	}

	close(eventsCh)
	var gotEvents []*sentry.Event
	for e := range eventsCh {
		gotEvents = append(gotEvents, e)
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
	if diff := cmp.Diff(wantEvents, gotEvents, opts); diff != "" {
		t.Fatalf("Events mismatch (-want +gotEvents):\n%s", diff)
	}

	close(transactionsCh)
	var gotTransactions []*sentry.Event
	for e := range transactionsCh {
		gotTransactions = append(gotTransactions, e)
	}
	optstrans := cmp.Options{
		cmpopts.IgnoreFields(
			sentry.Event{},
			"Contexts", "EventID", "Platform", "Modules",
			"Release", "Sdk", "ServerName", "Timestamp",
			"sdkMetaData", "StartTime", "Spans",
		),
		cmpopts.IgnoreFields(
			sentry.Request{},
			"Env",
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
	if diff := cmp.Diff(wantTransactions, gotTransactions, optstrans); diff != "" {
		t.Fatalf("Transactions mismatch (-want +gotEvents):\n%s", diff)
	}

	ln.Close()
	<-done
}

func TestGetTransactionFromContext(t *testing.T) {
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("With Transaction", func(t *testing.T) {
		sentryHandler := sentryfasthttp.New(sentryfasthttp.Options{})
		ln := fasthttputil.NewInmemoryListener()
		handler := func(ctx *fasthttp.RequestCtx) {
			span := sentryfasthttp.GetSpanFromContext(ctx)
			if span == nil {
				t.Error("expecting span to be not nil")
			}

			ctx.SetStatusCode(200)
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

		req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
		req.SetHost("example.com")
		req.URI().SetPath("/")
		req.Header.SetMethod("GET")
		if err := c.Do(req, res); err != nil {
			t.Fatalf("Request failed: %s", err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Errorf("Status code = %d", res.StatusCode())
		}

		ln.Close()
		<-done
	})

	t.Run("Without Transaction", func(t *testing.T) {
		ln := fasthttputil.NewInmemoryListener()
		handler := func(ctx *fasthttp.RequestCtx) {
			span := sentryfasthttp.GetSpanFromContext(ctx)
			if span != nil {
				t.Error("expecting span to be nil")
			}

			ctx.SetStatusCode(200)
		}
		done := make(chan struct{})
		go func() {
			if err := fasthttp.Serve(ln, handler); err != nil {
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

		req, res := fasthttp.AcquireRequest(), fasthttp.AcquireResponse()
		req.SetHost("example.com")
		req.URI().SetPath("/")
		req.Header.SetMethod("GET")
		if err := c.Do(req, res); err != nil {
			t.Fatalf("Request failed: %s", err)
		}
		if res.StatusCode() != http.StatusOK {
			t.Errorf("Status code = %d", res.StatusCode())
		}

		ln.Close()
		<-done
	})
}
