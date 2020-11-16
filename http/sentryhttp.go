// Package sentryhttp provides Sentry integration for servers based on the
// net/http package.
package sentryhttp

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
)

// A Handler is an HTTP middleware factory that provides integration with
// Sentry.
type Handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

// Options configure a Handler.
type Options struct {
	// Repanic configures whether Sentry should repanic after recovery
	Repanic bool
	// WaitForDelivery indicates whether to wait until panic details have been
	// sent to Sentry before panicking or proceeding with a request.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a new Handler. Use the Handle and HandleFunc methods to wrap
// existing HTTP handlers.
func New(options Options) *Handler {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return &Handler{
		repanic:         options.Repanic,
		timeout:         timeout,
		waitForDelivery: options.WaitForDelivery,
	}
}

// Handle works as a middleware that wraps an existing http.Handler. A wrapped
// handler will recover from and report panics to Sentry, and provide access to
// a request-specific hub to report messages and errors.
func (h *Handler) Handle(handler http.Handler) http.Handler {
	return h.handle(handler)
}

// HandleFunc is like Handle, but with a handler function parameter for cases
// where that is convenient. In particular, use it to wrap a handler function
// literal.
//
//  http.Handle(pattern, h.HandleFunc(func (w http.ResponseWriter, r *http.Request) {
//      // handler code here
//  }))
func (h *Handler) HandleFunc(handler http.HandlerFunc) http.HandlerFunc {
	return h.handle(handler)
}

func (h *Handler) handle(handler http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}
		hub.Scope().SetRequest(r)
		ctx = sentry.SetHubOnContext(ctx, hub)
		defer h.recoverWithSentry(hub, r)
		handler.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (h *Handler) recoverWithSentry(hub *sentry.Hub, r *http.Request) {
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
