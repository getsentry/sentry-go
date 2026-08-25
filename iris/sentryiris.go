package sentryiris

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/kataras/iris/v12"
)

// The identifier of the Iris SDK.
const (
	// sdkIdentifier is the identifier of the Iris SDK.
	sdkIdentifier = "sentry.go.iris"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as iris.Default includes it's own Recovery middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because Iris's default Recovery handler doesn't restart the application,
	// it's safe to either skip this option or set it to false.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a function that satisfies iris.Handler interface
// It can be used with Use() method.
func New(options Options) iris.Handler {
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(ctx iris.Context) {
	r := ctx.Request()
	requestCtx, scope := sentry.WithIsolationScope(r.Context())

	if client := sentry.GetClient(requestCtx); client.IsEnabled() {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	options := []sentry.SpanOption{
		sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(sentry.SourceRoute),
		sentry.WithSpanOrigin(sentry.SpanOriginIris),
	}

	currentRoute := ctx.GetCurrentRoute()

	transaction := sentry.StartTransaction(
		requestCtx,
		fmt.Sprintf("%s %s", currentRoute.Method(), currentRoute.Path()),
		options...,
	)

	defer func() {
		transaction.SetData("http.response.status_code", ctx.GetStatusCode())
		transaction.Status = sentry.HTTPtoSpanStatus(ctx.GetStatusCode())
		transaction.Finish()
	}()

	transaction.SetData("http.request.method", r.Method)

	r = r.WithContext(transaction.Context())
	ctx.ResetRequest(r)
	scope.SetRequest(r)
	defer h.recoverWithSentry(r)
	ctx.Next()
}

func (h *handler) recoverWithSentry(r *http.Request) {
	if err := recover(); err != nil {
		ctx := context.WithValue(r.Context(), sentry.RequestContextKey, r)
		eventID := sentry.CapturePanic(ctx, err)
		if eventID != nil && h.waitForDelivery {
			sentry.GetClient(ctx).Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}

// GetScopeFromContext retrieves the isolation scope from iris.Context.
func GetScopeFromContext(ctx iris.Context) *sentry.Scope {
	return sentry.ScopeFromContext(ctx.Request().Context())
}

// GetSpanFromContext retrieves the active span from iris.Context.
// If there is no transaction on iris.Context, it will return nil.
func GetSpanFromContext(ctx iris.Context) *sentry.Span {
	return sentry.SpanFromContext(ctx.Request().Context())
}
