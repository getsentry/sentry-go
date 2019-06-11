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

func New(options Options) martini.Handler {
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

func (h *Handler) handle() martini.Handler {
	return func(rw http.ResponseWriter, r *http.Request, c martini.Context) {
		hub := sentry.CurrentHub().Clone()
		c.Map(hub)
		defer h.recoverWithSentry(hub, r)
		c.Next()
	}
}

func (h *Handler) recoverWithSentry(hub *sentry.Hub, r *http.Request) {
	if err := recover(); err != nil {
		hub.Scope().SetRequest(sentry.Request{}.FromHTTPRequest(r))
		eventID := hub.RecoverWithContext(
			context.WithValue(r.Context(), sentry.RequestContextKey, r),
			err,
		)
		if eventID != nil && h.waitForDelivery {
			hub.Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}
