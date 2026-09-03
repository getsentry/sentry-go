package sentryfiber

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	fiber "github.com/gofiber/fiber/v3"
	"github.com/gofiber/utils/v2"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

const (
	// sdkIdentifier is the identifier of the Fiber SDK.
	sdkIdentifier = "sentry.go.fiberv3"
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

func (h *handler) handle(ctx fiber.Ctx) error {
	savedCtx := ctx.Context()
	requestCtx, cancel := context.WithCancel(savedCtx)
	storedCtx := &storedContext{ctx: requestCtx, cancel: cancel}
	ctx.Locals(contextKey{}, storedCtx)
	defer func() {
		if ctx.IsAbandoned() {
			cancel()
		}
	}()
	defer ctx.SetContext(savedCtx)

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
	ctx.SetContext(requestCtx)

	defer func() {
		if routePath := ctx.Route().Path; routePath != "" && !ctx.IsMiddleware() {
			transaction.Name = fmt.Sprintf("%s %s", r.Method, routePath)
			transaction.Source = sentry.SourceRoute
		}
		status := ctx.Response().StatusCode()
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	transaction.SetData("http.request.method", r.Method)
	r = r.WithContext(transaction.Context())

	scope.SetRequest(r)
	scope.SetRequestBody(bytes.Clone(ctx.Body()))
	defer h.recoverWithSentry(requestCtx, ctx, cancel)

	return ctx.Next()
}

func (h *handler) recoverWithSentry(requestCtx context.Context, ctx fiber.Ctx, cancel context.CancelFunc) {
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

// GetContext returns the request's Sentry context. Unlike Ctx.Context, it
// remains available to outer middleware and custom error handlers after the
// Sentry middleware restores Fiber's original context.
func GetContext(ctx fiber.Ctx) context.Context {
	if storedCtx, ok := ctx.Locals(contextKey{}).(*storedContext); ok {
		return storedCtx.ctx
	}
	return ctx.Context()
}

func convert(ctx fiber.Ctx) *http.Request {
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

	r.Header = make(http.Header)
	r.Header.Add("Host", host)

	for key, value := range ctx.Request().Header.All() {
		r.Header.Add(string(key), string(value))
	}

	for key, value := range ctx.Request().Header.Cookies() {
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)}) //nolint:gosec // G124: mirrors an inbound request cookie.
	}

	r.RemoteAddr = ctx.RequestCtx().RemoteAddr().String()

	return r
}
