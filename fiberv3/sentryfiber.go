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
	defer cancel()
	defer ctx.SetContext(savedCtx)

	requestCtx, scope := sentry.WithIsolationScope(requestCtx)

	if client := sentry.GetClient(requestCtx); client.IsEnabled() {
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
	ctx.SetContext(requestCtx)
	ctx.Locals(contextKey{}, requestCtx)

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
	defer h.recoverWithSentry(ctx)

	return ctx.Next()
}

func (h *handler) recoverWithSentry(ctx fiber.Ctx) {
	if err := recover(); err != nil {
		requestCtx := context.WithValue(GetContext(ctx), sentry.RequestContextKey, ctx)
		eventID := sentry.CapturePanic(requestCtx, err)
		if eventID != nil && h.waitForDelivery {
			sentry.GetClient(requestCtx).Flush(h.timeout)
		}
		if h.repanic {
			panic(err)
		}
	}
}

// GetContext retrieves the request context from the fiber.Ctx.
func GetContext(ctx fiber.Ctx) context.Context {
	if requestCtx, ok := ctx.Locals(contextKey{}).(context.Context); ok {
		return requestCtx
	}
	return ctx.Context()
}

// GetScopeFromContext retrieves the isolation scope from the fiber.Ctx.
func GetScopeFromContext(ctx fiber.Ctx) *sentry.Scope {
	return sentry.ScopeFromContext(GetContext(ctx))
}

// GetSpanFromContext retrieves the active span from the fiber.Ctx.
func GetSpanFromContext(ctx fiber.Ctx) *sentry.Span {
	return sentry.SpanFromContext(GetContext(ctx))
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
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)})
	}

	r.RemoteAddr = ctx.RequestCtx().RemoteAddr().String()

	return r
}
