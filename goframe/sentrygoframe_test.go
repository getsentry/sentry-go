package sentrygoframe_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	sentrygoframe "github.com/getsentry/sentry-go/goframe"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestIntegration(t *testing.T) {
	largePayload := strings.Repeat("Large", 3*1024) // 15 KB

	tests := []struct {
		RequestPath string
		RoutePath   string
		Method      string
		WantStatus  int
		Body        string
		Handler     ghttp.HandlerFunc

		WantEvent       *sentry.Event
		WantTransaction *sentry.Event
	}{
		{
			RequestPath: "/panic/1",
			RoutePath:   "/panic/:id",
			Method:      "GET",
			WantStatus:  200,
			Handler: func(r *ghttp.Request) {
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
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /panic/:id",
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
		},
		{
			RequestPath: "/404/1",
			RoutePath:   "",
			Method:      "GET",
			WantStatus:  404,
			Handler:     nil,
			WantEvent:   nil,
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "GET /404/1",
				Request: &sentry.Request{
					URL:    "/404/1",
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
							"http.response.status_code": http.StatusNotFound,
						},
						Op:     "http.server",
						Status: sentry.SpanStatusNotFound,
					}.Map(),
				}},
		},
		{
			RequestPath: "/post",
			RoutePath:   "/post",
			Method:      "POST",
			WantStatus:  200,
			Body:        "payload",
			Handler: func(r *ghttp.Request) {
				hub := sentry.GetHubFromContext(r.GetCtx())
				body, err := io.ReadAll(r.Request.Body)
				if err != nil {
					t.Error(err)
				}
				hub.CaptureMessage("post: " + string(body))
				r.Response.WriteJson(g.Map{"status": "ok"})
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
				}},
		},
		{
			RequestPath: "/get",
			RoutePath:   "/get",
			Method:      "GET",
			WantStatus:  200,
			Handler: func(r *ghttp.Request) {
				hub := sentry.GetHubFromContext(r.GetCtx())
				hub.CaptureMessage("get")
				r.Response.WriteJson(g.Map{"status": "get"})
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
				}},
		},
		{
			RequestPath: "/large",
			RoutePath:   "/large",
			Method:      "POST",
			WantStatus:  200,
			Body:        largePayload,
			Handler: func(r *ghttp.Request) {
				hub := sentry.GetHubFromContext(r.GetCtx())
				body, err := io.ReadAll(r.Request.Body)
				if err != nil {
					t.Error(err)
				}
				bodyLength := len(body)
				hub.CaptureMessage(fmt.Sprintf("Large request: %d bytes", bodyLength))
				r.Response.WriteJson(g.Map{"status": "large", "size": bodyLength})
			},
			WantEvent: &sentry.Event{
				Level:   sentry.LevelInfo,
				Message: fmt.Sprintf("Large request: %d bytes", len(largePayload)),
				Request: &sentry.Request{
					URL:    "/large",
					Method: "POST",
					Data:   "",
					Headers: map[string]string{
						"Accept-Encoding": "gzip",
						"Content-Length":  strconv.Itoa(len(largePayload)),
						"User-Agent":      "Go-http-client/1.1",
					},
				},
			},
			WantTransaction: &sentry.Event{
				Level:       sentry.LevelInfo,
				Type:        "transaction",
				Transaction: "POST /large",
				Request: &sentry.Request{
					URL:    "/large",
					Method: "POST",
					Data:   "",
					Headers: map[string]string{
						"Content-Length":  strconv.Itoa(len(largePayload)),
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
				}},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s %s", tt.Method, tt.RequestPath), func(t *testing.T) {
			transport := testutils.NewTransport()
			transportWithState := testutils.TransportWithState{
				Transport: transport,
			}
			client, err := sentry.NewClient(sentry.ClientOptions{
				Transport: &transportWithState,
			})
			if err != nil {
				t.Fatal(err)
			}

			hub := sentry.NewHub(client, sentry.NewScope())
			integration := sentrygoframe.New(sentrygoframe.Options{
				Repanic:         false,
				WaitForDelivery: true,
			})

			s := g.Server()
			s.SetDumpRouterMap(false)
			s.Use(func(r *ghttp.Request) {
				r.SetCtx(sentry.SetHubOnContext(r.GetCtx(), hub))
				r.Middleware.Next()
			})
			s.Use(integration)

			if tt.Handler != nil && tt.RoutePath != "" {
				s.BindHandler(tt.RoutePath, tt.Handler)
			}

			httpServer := httptest.NewServer(s)
			defer httpServer.Close()

			req, err := http.NewRequest(
				tt.Method,
				httpServer.URL+tt.RequestPath,
				strings.NewReader(tt.Body),
			)
			if err != nil {
				t.Fatal(err)
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()

			if res.StatusCode != tt.WantStatus {
				t.Errorf("expected status code %d, got %d", tt.WantStatus, res.StatusCode)
			}

			var event, transaction *sentry.Event

			for i := 0; i < len(transportWithState.Events()); i++ {
				e := transportWithState.Events()[i]
				if e.Type == "transaction" {
					transaction = e
				} else {
					event = e
				}
			}

			if diff := cmpEvent(tt.WantEvent, event); diff != "" {
				t.Errorf("Event mismatch (-want +got):\n%s", diff)
			}

			if diff := cmpEvent(tt.WantTransaction, transaction); diff != "" {
				t.Errorf("Transaction mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetHubFromContext(t *testing.T) {
	s := g.Server()
	integration := sentrygoframe.New(sentrygoframe.Options{
		Repanic:         false,
		WaitForDelivery: true,
	})
	s.Use(integration)

	called := false
	s.BindHandler("/hub", func(r *ghttp.Request) {
		called = true
		hub := sentry.GetHubFromContext(r.GetCtx())
		if hub == nil {
			t.Error("Expected hub to not be nil")
		}
		r.Response.Write("ok")
	})

	httpServer := httptest.NewServer(s)
	defer httpServer.Close()

	req, err := http.NewRequest("GET", httpServer.URL+"/hub", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if !called {
		t.Error("Expected handler to be called")
	}
}

func TestGetValueFromContext(t *testing.T) {
	s := g.Server()
	integration := sentrygoframe.New(sentrygoframe.Options{
		Repanic:         false,
		WaitForDelivery: true,
	})
	s.Use(integration)

	called := false
	s.BindHandler("/values", func(r *ghttp.Request) {
		called = true
		value := r.GetCtxVar("sentry")
		if value.IsEmpty() {
			t.Error("Expected value to not be empty")
		}
		value = r.GetCtxVar("sentry_transaction")
		if value.IsEmpty() {
			t.Error("Expected transaction value to not be empty")
		}
		r.Response.Write("ok")
	})

	httpServer := httptest.NewServer(s)
	defer httpServer.Close()

	req, err := http.NewRequest("GET", httpServer.URL+"/values", nil)
	if err != nil {
		t.Fatal(err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if !called {
		t.Error("Expected handler to be called")
	}
}

// cmpEvent compares want and got events
func cmpEvent(want, got *sentry.Event) string {
	if want == nil && got == nil {
		return ""
	}
	if want == nil && got != nil {
		return "want nil event, got non-nil event"
	}
	if want != nil && got == nil {
		return "want non-nil event, got nil event"
	}

	if want.Type == "transaction" && got.Type == "transaction" {
		if want.TransactionInfo != nil && got.TransactionInfo != nil {
			if want.TransactionInfo.Source != "" && got.TransactionInfo.Source == "" {
				return fmt.Sprintf("transaction source: want %q, got %q", want.TransactionInfo.Source, got.TransactionInfo.Source)
			}
		}
	}

	opts := []cmp.Option{
		cmpopts.IgnoreFields(sentry.Event{}, "Contexts", "EventID", "Extra", "Platform", "Modules", "Release", "Debug", "Timestamp", "StartTimestamp", "EndTimestamp", "SDK", "ServerName", "Tags"),
		cmpopts.IgnoreMapEntries(func(k string, v interface{}) bool {
			return k != "http.request.method" && k != "http.response.status_code"
		}),
	}

	return cmp.Diff(want, got, opts...)
}
