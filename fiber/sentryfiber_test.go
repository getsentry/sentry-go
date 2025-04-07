package sentryfiber_test

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/getsentry/sentry-go/fiber"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB
	sentryHandler := sentryfiber.New(sentryfiber.Options{Timeout: 3 * time.Second, WaitForDelivery: true})
	exception := errors.New("unknown error")

	app := fiber.New(fiber.Config{
		ErrorHandler: func(c *fiber.Ctx, e error) error {
			hub := sentryfiber.GetHubFromContext(c)
			hub.CaptureException(e)
			return nil
		},
	})

	app.Use(sentryHandler)

	app.Get("/panic", func(c *fiber.Ctx) error {
		panic("test")
	})
	app.Post("/post", func(c *fiber.Ctx) error {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("post: " + string(c.Body()))
		return nil
	})

	app.Get("/get", func(c *fiber.Ctx) error {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("get")
		return nil
	})

	app.Get("/get/:id", func(c *fiber.Ctx) error {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage(fmt.Sprintf("get: %s", c.Params("id")))
		return nil
	})

	app.Post("/post/large", func(c *fiber.Ctx) error {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(c.Body())/1024))
		return nil
	})
	app.Post("/post/body-ignored", func(c *fiber.Ctx) error {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("body ignored")
		return nil
	})
	app.Post("/post/error-handler", func(c *fiber.Ctx) error {
		return exception
	})

	tests := []struct {
		Path            string
		Method          string
		Body            string
		WantStatus      int
		WantEvent       *sentry.Event
		WantTransaction *sentry.Event
	}{
		{
			Path:       "/panic",
			Method:     "GET",
			WantStatus: 200,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelFatal,
				Message: "test",
				Request: &sentry.Request{
					URL:    "http://example.com/panic",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /panic",
				Request: &sentry.Request{
					URL:    "http://example.com/panic",
					Method: http.MethodGet,
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
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
			Path:       "/post",
			Method:     "POST",
			Body:       "payload",
			WantStatus: 200,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: payload",
				Request: &sentry.Request{
					URL:    "http://example.com/post",
					Method: "POST",
					Data:   "payload",
					Headers: map[string]string{
						"Content-Length": "7",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post",
				Request: &sentry.Request{
					URL:    "http://example.com/post",
					Method: http.MethodPost,
					Data:   "payload",
					Headers: map[string]string{
						"Host":           "example.com",
						"Content-Length": "7",
						"User-Agent":     "fiber",
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
			Path:       "/get",
			Method:     "GET",
			WantStatus: 200,

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "get",
				Request: &sentry.Request{
					URL:    "http://example.com/get",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /get",
				Request: &sentry.Request{
					URL:    "http://example.com/get",
					Method: http.MethodGet,
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
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
			Path:       "/get/123",
			Method:     "GET",
			WantStatus: 200,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "get: 123",
				Request: &sentry.Request{
					URL:    "http://example.com/get/123",
					Method: "GET",
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /get/123",
				Request: &sentry.Request{
					URL:    "http://example.com/get/123",
					Method: http.MethodGet,
					Headers: map[string]string{
						"Host":       "example.com",
						"User-Agent": "fiber",
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
			Path:       "/post/large",
			Method:     "POST",
			WantStatus: 200,
			Body:       largePayload,
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "post: 15 KB",
				Request: &sentry.Request{
					URL:    "http://example.com/post/large",
					Method: "POST",
					// Actual request body omitted because too large.
					Data: "",
					Headers: map[string]string{
						"Content-Length": "15360",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/large",
				Request: &sentry.Request{
					URL:    "http://example.com/post/large",
					Method: http.MethodPost,
					Data:   "",
					Headers: map[string]string{
						"Content-Length": "15360",
						"Host":           "example.com",
						"User-Agent":     "fiber",
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
			Path:       "/post/body-ignored",
			WantStatus: 200,
			Method:     "POST",
			Body:       "client sends, fiber always reads, SDK reports",

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "body ignored",
				Request: &sentry.Request{
					URL:    "http://example.com/post/body-ignored",
					Method: "POST",
					Data:   "client sends, fiber always reads, SDK reports",
					Headers: map[string]string{
						"Content-Length": "45",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/body-ignored",
				Request: &sentry.Request{
					URL:    "http://example.com/post/body-ignored",
					Method: http.MethodPost,
					Data:   "client sends, fiber always reads, SDK reports",
					Headers: map[string]string{
						"Content-Length": "45",
						"Host":           "example.com",
						"User-Agent":     "fiber",
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
			Path:       "/post/error-handler",
			Method:     "POST",
			WantStatus: 200,

			WantEvent: &sentry.Event{
				Level: sentry.LevelError,
				Exception: []sentry.Exception{
					{
						Value: exception.Error(),
						Type:  reflect.TypeOf(exception).String(),
					},
				},
				Request: &sentry.Request{
					URL:    "http://example.com/post/error-handler",
					Method: "POST",
					Headers: map[string]string{
						"Content-Length": "0",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /post/error-handler",
				Request: &sentry.Request{
					URL:    "http://example.com/post/error-handler",
					Method: http.MethodPost,
					Headers: map[string]string{
						"Host":           "example.com",
						"User-Agent":     "fiber",
						"Content-Length": "0",
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

	var wantEvents []*sentry.Event
	var wantTransactions []*sentry.Event
	var wantCodes []sentry.SpanStatus
	for _, tt := range tests {
		wantEvents = append(wantEvents, tt.WantEvent)
		wantTransactions = append(wantTransactions, tt.WantTransaction)
		wantCodes = append(wantCodes, sentry.HTTPtoSpanStatus(tt.WantStatus))
		req, err := http.NewRequest(tt.Method, "http://example.com"+tt.Path, strings.NewReader(tt.Body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("User-Agent", "fiber")
		resp, err := app.Test(req)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("Request %q failed: %s", tt.Path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Status code = %d", resp.StatusCode)
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

	opt := cmp.Options{
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
		cmpopts.IgnoreFields(
			sentry.Exception{},
			"Stacktrace",
		),
	}
	if diff := cmp.Diff(wantEvents, gotEvents, opt); diff != "" {
		t.Fatalf("Events mismatch (-want +got):\n%s", diff)
	}

	close(transactionsCh)
	var gotTransactions []*sentry.Event
	var gotCodes []sentry.SpanStatus

	for e := range transactionsCh {
		gotTransactions = append(gotTransactions, e)
		gotCodes = append(gotCodes, e.Contexts["trace"]["status"].(sentry.SpanStatus))
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
		cmpopts.IgnoreFields(
			sentry.Exception{},
			"Stacktrace",
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

	if diff := cmp.Diff(wantTransactions, gotTransactions, optstrans); diff != "" {
		t.Fatalf("Transactions mismatch (-want +gotEvents):\n%s", diff)
	}

	if diff := cmp.Diff(wantCodes, gotCodes, cmp.Options{}); diff != "" {
		t.Fatalf("Transaction status codes mismatch (-want +got):\n%s", diff)
	}
}

func TestHandlers(t *testing.T) {
	err := sentry.Init(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		useSentry bool
	}{
		"With Transaction":    {useSentry: true},
		"Without Transaction": {useSentry: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a new Fiber app
			app := fiber.New()

			if tc.useSentry {
				sentryHandler := sentryfiber.New(sentryfiber.Options{Timeout: 3 * time.Second, WaitForDelivery: true})
				app.Use(sentryHandler)
			}

			handler := func(ctx *fiber.Ctx) error {
				span := sentryfiber.GetSpanFromContext(ctx)
				if tc.useSentry && span == nil {
					t.Error("expecting span not to be nil")
				}
				if !tc.useSentry && span != nil {
					t.Error("expecting span to be nil")
				}
				return nil
			}

			app.Get("/hello", handler)
			req, err := http.NewRequest(http.MethodGet, "/hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("User-Agent", "fiber")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("Request failed: %s", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
			}
		})
	}
}

func TestSetHubOnContext(t *testing.T) {
	app := fiber.New()
	hub := sentry.NewHub(sentry.CurrentHub().Client(), sentry.NewScope())

	app.Get("/test", func(c *fiber.Ctx) error {
		sentryfiber.SetHubOnContext(c, hub)
		retrievedHub := sentryfiber.GetHubFromContext(c)
		if retrievedHub == nil {
			t.Fatal("expected hub to be set on context, but got nil")
		}
		if !reflect.DeepEqual(hub, retrievedHub) {
			t.Fatalf("expected hub to be %v, but got %v", hub, retrievedHub)
		}
		return nil
	})

	req, err := http.NewRequest(http.MethodGet, "/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "fiber")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("Request failed: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code %d, got %d", http.StatusOK, resp.StatusCode)
	}
}
