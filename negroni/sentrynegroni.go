package sentrynegroni

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/urfave/negroni"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as negroni.Classic includes it's own Recovery middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because Negroni's default Recovery handler doesn't restart the application,
	// it's safe to either skip this option or set it to false.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a handler struct which satisfies Negroni's middleware interface
// It can be used with New(), Use() or With() methods.
func New(options Options) negroni.Handler {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &handler{
		repanic:         options.Repanic,
		timeout:         timeout,
		waitForDelivery: options.WaitForDelivery,
	}
}

func (h *handler) ServeHTTP(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	ctx := r.Context()
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}
	hub.Scope().SetRequest(r)
	ctx = sentry.SetHubOnContext(
		context.WithValue(ctx, sentry.RequestContextKey, r),
		hub,
	)
	defer h.recoverWithSentry(hub, r)
	next(rw, r.WithContext(ctx))
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, r *http.Request) {
	if err := recover(); err != nil {
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

// PanicHandlerFunc can be used for Negroni's default Recovery middleware option called PanicHandlerFunc,
// which let you "plug-in" to it's own handler.
func PanicHandlerFunc(info *negroni.PanicInformation) {
	hub := sentry.CurrentHub().Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetRequest(info.Request)
		hub.RecoverWithContext(
			context.WithValue(context.Background(), sentry.RequestContextKey, info.Request),
			info.RecoveredPanic,
		)
	})
}
