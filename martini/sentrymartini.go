package sentrymartini

import (
	"context"
	"net/http"
	"time"

	"github.com/go-martini/martini"

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

func (h *Handler) Handle() martini.Handler {
	return func(rw http.ResponseWriter, r *http.Request, c martini.Context) {
		ctx := sentry.SetHubOnContext(
			context.WithValue(r.Context(), sentry.RequestContextKey, r),
			sentry.CurrentHub().Clone(),
		)
		defer h.recoverWithSentry(ctx, r)
		c.Next()
	}
}

func (h *Handler) recoverWithSentry(ctx context.Context, r *http.Request) {
	if err := recover(); err != nil {
		hub := sentry.GetHubFromContext(ctx)
		hub.ConfigureScope(func(scope *sentry.Scope) {
			scope.SetRequest(sentry.Request{}.FromHTTPRequest(r))
		})
		eventID := hub.RecoverWithContext(ctx, err)
		if eventID != nil && h.waitForDelivery {
			hub.Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}
