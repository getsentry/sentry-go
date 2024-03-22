package sentryecho

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v4"
)

const (
	// The identifier of the Echo SDK.
	sdkIdentifier = "sentry.go.echo"

	valuesKey = "sentry"

	transactionKey = "sentry_transaction"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as echo includes it's own Recover middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because Echo's Recover handler doesn't restart the application,
	// it's safe to either skip this option or set it to false.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a function that satisfies echo.HandlerFunc interface
// It can be used with Use() methods.
func New(options Options) echo.MiddlewareFunc {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	return (&handler{
		repanic:         options.Repanic,
		timeout:         timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx echo.Context) error {
		r := ctx.Request()
		stdCtx := r.Context()

		hub := sentry.GetHubFromContext(r.Context())
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
			stdCtx = sentry.SetHubOnContext(stdCtx, hub)
		}

		if client := hub.Client(); client != nil {
			client.SetSDKIdentifier(sdkIdentifier)
		}

		transactionName := r.URL.Path
		transactionSource := sentry.SourceURL

		if path := ctx.Path(); path != "" {
			transactionName = path
			transactionSource = sentry.SourceRoute
		}

		options := []sentry.SpanOption{
			sentry.WithOpName("http.server"),
			sentry.ContinueFromRequest(r),
			sentry.WithTransactionSource(transactionSource),
		}

		transaction := sentry.StartTransaction(stdCtx,
			fmt.Sprintf("%s %s", r.Method, transactionName),
			options...,
		)
		defer func() {
			transaction.Status = sentry.HTTPtoSpanStatus(ctx.Response().Status)
			transaction.Finish()
		}()

		r = r.WithContext(transaction.Context())
		hub.Scope().SetRequest(r)
		ctx.Set(valuesKey, hub)
		ctx.Set(transactionKey, transaction)
		defer h.recoverWithSentry(hub, r)
		return next(ctx)
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

// GetHubFromContext retrieves attached *sentry.Hub instance from echo.Context.
func GetHubFromContext(ctx echo.Context) *sentry.Hub {
	if hub, ok := ctx.Get(valuesKey).(*sentry.Hub); ok {
		return hub
	}
	return nil
}

// GetSpanFromContext retrieves attached *sentry.Span instance from echo.Context.
// If there is no transaction on echo.Context, it will return nil.
func GetSpanFromContext(ctx echo.Context) *sentry.Span {
	if span, ok := ctx.Get(transactionKey).(*sentry.Span); ok {
		return span
	}
	return nil
}
