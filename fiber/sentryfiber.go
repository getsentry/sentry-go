package sentryfiber

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

const (
	// sdkIdentifier is the identifier of the FastHTTP SDK.
	sdkIdentifier = "sentry.go.fiber"
)

type contextKey struct{}

type storedContext struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (ctx *storedContext) Close() error {
	ctx.cancel()
	return nil
}

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to false,
	// as fasthttp doesn't include its own Recovery handler.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because fasthttp doesn't include its own Recovery handler, it will restart the application,
	// and event won't be delivered otherwise.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a handler struct which satisfies Fiber's middleware interface.
func New(options Options) fiber.Handler {
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(ctx *fiber.Ctx) error {
	savedCtx := ctx.UserContext()
	requestCtx, cancel := context.WithCancel(savedCtx)
	storedCtx := &storedContext{ctx: requestCtx, cancel: cancel}
	ctx.Locals(contextKey{}, storedCtx)
	defer func() {
		if ctx.Context().LastTimeoutErrorResponse() != nil {
			cancel()
		}
	}()
	defer func() { ctx.SetUserContext(savedCtx) }()

	requestCtx, scope := sentry.WithIsolationScope(requestCtx)

	if client := sentry.ClientFromContext(requestCtx); client.IsEnabled() {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	r := convert(ctx)

	transactionName := ctx.Path()
	transactionSource := sentry.SourceURL

	options := []sentry.SpanOption{
		sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(transactionSource),
		sentry.WithSpanOrigin(sentry.SpanOriginFiber),
	}

	transaction := sentry.StartTransaction(
		requestCtx,
		fmt.Sprintf("%s %s", r.Method, transactionName),
		options...,
	)
	requestCtx = transaction.Context()
	storedCtx.ctx = requestCtx
	ctx.SetUserContext(requestCtx)

	defer func() {
		// Fiber v2 does not expose whether ctx.Route() originates from a middleware. We keep the
		// URL-based name (opposite to v3) because middlewares that short-circuits the handler chain can
		// otherwise replace the ctx.Route(). See https://github.com/getsentry/sentry-go/issues/1361.
		status := ctx.Response().StatusCode()
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	transaction.SetData("http.request.method", r.Method)
	r = r.WithContext(transaction.Context())

	scope.SetRequest(r)
	scope.SetRequestBody(bytes.Clone(ctx.Request().Body()))
	defer h.recoverWithSentry(requestCtx, ctx, cancel)

	return ctx.Next()
}

func (h *handler) recoverWithSentry(requestCtx context.Context, ctx *fiber.Ctx, cancel context.CancelFunc) {
	if err := recover(); err != nil {
		requestCtx = context.WithValue(requestCtx, sentry.RequestContextKey, ctx)
		eventID := sentry.Recover(requestCtx, err)
		if eventID != nil && h.waitForDelivery {
			sentry.ClientFromContext(requestCtx).Flush(h.timeout)
		}
		if h.repanic {
			cancel()
			panic(err)
		}
	}
}

// GetContext returns the request's Sentry context. Unlike Ctx.UserContext, it
// remains available to outer middleware and custom error handlers after the
// Sentry middleware restores Fiber's original context.
func GetContext(ctx *fiber.Ctx) context.Context {
	if storedCtx, ok := ctx.Locals(contextKey{}).(*storedContext); ok {
		return storedCtx.ctx
	}
	return ctx.UserContext()
}

func convert(ctx *fiber.Ctx) *http.Request {
	defer func() {
		if err := recover(); err != nil {
			debuglog.Printf("%v", err)
		}
	}()

	r := new(http.Request)

	r.Method = utils.CopyString(ctx.Method())

	uri := ctx.Request().URI()
	r.URL = &url.URL{Path: string(uri.Path())}
	r.URL.RawQuery = string(uri.QueryString())

	if parsedURL, err := url.Parse(fmt.Sprintf("%s://%s%s", uri.Scheme(), uri.Host(), uri.Path())); err == nil {
		r.URL = parsedURL
		r.URL.RawQuery = string(uri.QueryString())
	}

	host := utils.CopyString(ctx.Hostname())
	r.Host = host

	// Headers
	r.Header = make(http.Header)
	r.Header.Add("Host", host)

	ctx.Request().Header.VisitAll(func(key, value []byte) { // nolint: staticcheck // this is intentional to support older versions of fasthttp for fiber v2
		r.Header.Add(string(key), string(value))
	})

	// Cookies
	ctx.Request().Header.VisitAllCookie(func(key, value []byte) { // nolint: staticcheck // this is intentional to support older versions of fasthttp for fiber v2
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)}) //nolint:gosec // G124: mirrors an inbound request cookie.
	})

	r.RemoteAddr = ctx.Context().RemoteAddr().String()

	return r
}
