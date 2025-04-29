package sentrygoframe

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
)

const (
	// sdkIdentifier is the identifier of the GoFrame SDK.
	sdkIdentifier = "sentry.go.goframe"

	// valuesKey is used as a key to store the Sentry Hub instance.
	valuesKey = "sentry"

	// transactionKey is used as a key to store the Sentry transaction.
	transactionKey = "sentry_transaction"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as GoFrame includes it's own Recovery middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because GoFrame's default Recovery handler doesn't restart the application,
	// it's safe to either skip this option or set it to false.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a function that satisfies ghttp.HandlerFunc interface
// It can be used with Use() methods.
func New(options Options) ghttp.HandlerFunc {
	if options.Timeout == 0 {
		options.Timeout = 2 * time.Second
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(r *ghttp.Request) {
	ctx := r.GetCtx()
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	if client := hub.Client(); client != nil {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	transactionName := r.Request.URL.Path
	transactionSource := sentry.SourceURL

	if path := r.GetUrl(); path != "" {
		transactionName = path
		transactionSource = sentry.SourceRoute
	}

	options := []sentry.SpanOption{
		sentry.ContinueTrace(hub, r.GetHeader(sentry.SentryTraceHeader), r.GetHeader(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(transactionSource),
		sentry.WithSpanOrigin(sentry.SpanOriginManual),
	}

	transaction := sentry.StartTransaction(
		sentry.SetHubOnContext(ctx, hub),
		fmt.Sprintf("%s %s", r.Request.Method, transactionName),
		options...,
	)

	transaction.SetData("http.request.method", r.Request.Method)

	defer func() {
		status := r.Response.Status
		if err := r.GetError(); err != nil {
			status = gerror.Code(err).Code()
		}
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	r.Request = r.Request.WithContext(transaction.Context())
	hub.Scope().SetRequest(r.Request)
	r.SetCtxVar(valuesKey, hub)
	r.SetCtxVar(transactionKey, transaction)
	defer h.recoverWithSentry(hub, r)

	r.Middleware.Next()
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, r *ghttp.Request) {
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
