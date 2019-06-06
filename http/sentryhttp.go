package sentryhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

type Handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	Repanic         bool
	WaitForDelivery bool
	Timeout         time.Duration
}

func New(options Options) *Handler {
	handler := Handler{
		repanic:         false,
		timeout:         time.Second * 2,
		waitForDelivery: false,
	}

	if options.Repanic {
		handler.repanic = true
	}

	if options.WaitForDelivery {
		handler.waitForDelivery = true
	}

	return &handler
}

func (h *Handler) Handle(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ctx := createContextWithHub(r)
		defer func() {
			if err := recover(); err != nil {
				hub := sentry.GetHubFromContext(ctx)
				hub.RecoverWithContext(ctx, err)
				if h.waitForDelivery {
					hub.Flush(h.timeout)
				}
				if h.repanic {
					panic(err)
				}
			}
		}()
		handler.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func (h *Handler) HandleFunc(handler http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		ctx := createContextWithHub(r)
		defer func() {
			if err := recover(); err != nil {
				sentry.GetHubFromContext(ctx).RecoverWithContext(ctx, err)
				hub := sentry.GetHubFromContext(ctx)
				hub.RecoverWithContext(ctx, err)
				if h.waitForDelivery {
					hub.Flush(h.timeout)
				}
				if h.repanic {
					panic(err)
				}
			}
		}()
		handler(rw, r.WithContext(ctx))
	}
}

func createContextWithHub(r *http.Request) context.Context {
	parentHub := sentry.CurrentHub()
	client := parentHub.Client()
	scope := parentHub.Scope().Clone()
	isolatedHub := sentry.NewHub(client, scope)

	scope.SetRequest(sentry.Request{}.FromHTTPRequest(r))

	ctx := r.Context()
	ctx = context.WithValue(ctx, sentry.RequestContextKey, r)
	return sentry.SetHubOnContext(ctx, isolatedHub)
}
