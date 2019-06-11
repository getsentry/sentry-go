package sentryiris

import (
	"context"
	"net/http"
	"time"

	"github.com/kataras/iris"

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

func New(options Options) func(ctx iris.Context) {
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

	return handler.handle()
}

func (h *Handler) handle() func(ctx iris.Context) {
	return func(ctx iris.Context) {
		r := ctx.Request()
		c := sentry.SetHubOnContext(
			context.WithValue(r.Context(), sentry.RequestContextKey, r),
			sentry.CurrentHub().Clone(),
		)
		defer h.recoverWithSentry(c, r)
		ctx.Next()
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
