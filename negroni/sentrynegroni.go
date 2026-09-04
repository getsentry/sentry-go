package sentrynegroni

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/httputils"
	"github.com/getsentry/sentry-go/internal/traceutils"
	"github.com/urfave/negroni/v3"
)

// The identifier of the Negroni SDK.
const sdkIdentifier = "sentry.go.negroni"

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as negroni.Classic includes it's own Recovery middleware that handles http responses.
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
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return &handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
	ctx, scope := sentry.WithIsolationScope(r.Context())

	if client := sentry.ClientFromContext(ctx); client.IsEnabled() {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	options := []sentry.SpanOption{
		sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(sentry.SourceURL),
		sentry.WithSpanOrigin(sentry.SpanOriginNegroni),
	}

	transaction := sentry.StartTransaction(
		ctx,
		traceutils.GetHTTPSpanName(r),
		options...,
	)

	transaction.SetData("http.request.method", r.Method)
	rw := httputils.NewWrapResponseWriter(w, r.ProtoMajor)

	defer func() {
		status := rw.Status()
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	r = r.WithContext(transaction.Context())
	scope.SetRequest(r)
	defer h.recoverWithSentry(r)

	next(rw, r)
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

// PanicHandlerFunc can be used for Negroni's default Recovery middleware option called PanicHandlerFunc,
// which let you "plug-in" to its own handler.
func PanicHandlerFunc(info *negroni.PanicInformation) {
	ctx, scope := sentry.WithIsolationScope(info.Request.Context())
	request := info.Request.WithContext(ctx)
	scope.SetRequest(request)
	ctx = context.WithValue(ctx, sentry.RequestContextKey, request)
	sentry.Recover(ctx, info.RecoveredPanic)
}
