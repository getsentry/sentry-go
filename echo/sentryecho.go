package sentryecho

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/labstack/echo/v5"
)

const (
	// sdkIdentifier is the identifier of the Echo SDK.
	sdkIdentifier = "sentry.go.echo"

	// errorKey is used as a key to store the error on the *echo.Context.
	errorKey = "error"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as Echo includes its own Recover middleware that handles HTTP responses.
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
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(next echo.HandlerFunc) echo.HandlerFunc {
	return func(ctx *echo.Context) error {
		r := ctx.Request()
		requestCtx, scope := sentry.WithIsolationScope(r.Context())

		if client := sentry.ClientFromContext(requestCtx); client.IsEnabled() {
			client.SetSDKIdentifier(sdkIdentifier)
		}

		transactionName := r.URL.Path
		transactionSource := sentry.SourceURL

		if path := ctx.Path(); path != "" {
			transactionName = path
			transactionSource = sentry.SourceRoute
		}

		options := []sentry.SpanOption{
			sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(transactionSource),
			sentry.WithSpanOrigin(sentry.SpanOriginEcho),
		}

		transaction := sentry.StartTransaction(
			requestCtx,
			fmt.Sprintf("%s %s", r.Method, transactionName),
			options...,
		)

		transaction.SetData("http.request.method", r.Method)

		defer func() {
			var status int
			if resp, err := echo.UnwrapResponse(ctx.Response()); err == nil && resp.Status != 0 {
				status = resp.Status
			}
			if err := ctx.Get(errorKey); err != nil {
				if coder, ok := err.(echo.HTTPStatusCoder); ok {
					status = coder.StatusCode()
				}
			}

			if status == 0 {
				debuglog.Printf("sentryecho: unable to determine HTTP response status code")
			} else {
				transaction.Status = sentry.HTTPtoSpanStatus(status)
				transaction.SetData("http.response.status_code", status)
			}
			transaction.Finish()
		}()

		r = r.WithContext(transaction.Context())
		ctx.SetRequest(r)
		scope.SetRequest(r)
		defer h.recoverWithSentry(r)

		err := next(ctx)
		if err != nil {
			// Store the error so it can be used in the deferred function
			ctx.Set(errorKey, err)
		}

		return err
	}
}

func (h *handler) recoverWithSentry(r *http.Request) {
	if err := recover(); err != nil {
		ctx := context.WithValue(r.Context(), sentry.RequestContextKey, r)
		eventID := sentry.Recover(ctx, err)
		if eventID != nil && h.waitForDelivery {
			sentry.ClientFromContext(ctx).Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}
