package sentrybuffalo

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gobuffalo/buffalo"
)

var sentryHubKey = "sentry_hub"

func SetSentryHubKey(key string) {
	sentryHubKey = key
}

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
	captureError    bool
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery
	Repanic bool
	// WaitForDelivery indicates whether to wait until panic details have been
	// sent to Sentry before panicking or proceeding with a request.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
	// CaptureError will capture the error if one was returned.
	CaptureError bool
}

// New returns a function that satisfies buffalo.MiddlewareFunc interface
// It can be used with Use() methods.
func New(options Options) buffalo.MiddlewareFunc {
	handler := handler{
		repanic:         false,
		timeout:         time.Second * 2,
		waitForDelivery: false,
		captureError:    false,
	}

	if options.Repanic {
		handler.repanic = true
	}

	if options.Timeout != 0 {
		handler.timeout = options.Timeout
	}

	if options.WaitForDelivery {
		handler.waitForDelivery = true
	}

	if options.CaptureError {
		handler.captureError = true
	}

	return handler.handle
}

func (h *handler) handle(next buffalo.Handler) buffalo.Handler {
	return func(c buffalo.Context) error {
		r := c.Request()
		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetRequest(sentry.Request{}.FromHTTPRequest(r))
		c.Set(sentryHubKey, hub)
		defer h.recoverWithSentry(hub, r)
		err := next(c)
		if err != nil && h.captureError {
			hub.CaptureException(err)
		}
		return err
	}
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

// GetHubFromContext returns sentry.Hub from the buffalo.Context
func GetHubFromContext(c buffalo.Context) *sentry.Hub {
	if hub, ok := c.Value(sentryHubKey).(*sentry.Hub); ok {
		return hub
	}
	return nil
}
