package sentryfiber_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/getsentry/sentry-go/fiber"
	"github.com/gofiber/fiber"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB
	sentryHandler := sentryfiber.New(sentryfiber.Options{})

	app := fiber.New()

	app.Use(sentryHandler)

	app.Get("/panic", func(c *fiber.Ctx) {
		panic("test")
	})
	app.Post("/post", func(c *fiber.Ctx) {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("post: " + c.Body())
	})
	app.Get("/get", func(c *fiber.Ctx) {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("get")
	})
	app.Post("/post/large", func(c *fiber.Ctx) {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage(fmt.Sprintf("post: %d KB", len(c.Body())/1024))
	})
	app.Post("/post/body-ignored", func(c *fiber.Ctx) {
		hub := sentryfiber.GetHubFromContext(c)
		hub.CaptureMessage("body ignored")
	})

	tests := []struct {
		Path   string
		Method string
		Body   string

		WantEvent *sentry.Event
	}{
		{
			Path:   "/panic",
			Method: "GET",

			WantEvent: &sentry.Event{
				Level:   sentry.LevelFatal,
				Message: "test",
				Request: &sentry.Request{
					URL:    "http://example.com/panic",
					Method: "GET",
					Headers: map[string]string{
						"Content-Length": "0",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
		},
		{
			Path:   "/post",
			Method: "POST",
			Body:   "payload",

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
		},
		{
			Path:   "/get",
			Method: "GET",

			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: "get",
				Request: &sentry.Request{
					URL:    "http://example.com/get",
					Method: "GET",
					Headers: map[string]string{
						"Content-Length": "0",
						"Host":           "example.com",
						"User-Agent":     "fiber",
					},
				},
			},
		},
		{
			Path:   "/post/large",
			Method: "POST",
			Body:   largePayload,

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
		},
		{
			Path:   "/post/body-ignored",
			Method: "POST",
			Body:   "client sends, fasthttp always reads, SDK reports",

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
						"Content-Length": "48",
						"Host":           "example.com",
						"User-Agent":     "fiber",
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

	var want []*sentry.Event
	for _, tt := range tests {
		want = append(want, tt.WantEvent)
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

	if ok := sentry.Flush(time.Second); !ok {
		t.Fatal("sentry.Flush timed out")
	}
	close(eventsCh)
	var got []*sentry.Event
	for e := range eventsCh {
		got = append(got, e)
	}
	opt := cmpopts.IgnoreFields(
		sentry.Event{},
		"Contexts", "EventID", "Extra", "Platform",
		"Sdk", "ServerName", "Tags", "Timestamp",
	)
	if diff := cmp.Diff(want, got, opt); diff != "" {
		t.Fatalf("Events mismatch (-want +got):\n%s", diff)
	}
}
