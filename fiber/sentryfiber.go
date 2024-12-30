package sentryfiber

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"

	"github.com/getsentry/sentry-go"
)

const (
	// sdkIdentifier is the identifier of the FastHTTP SDK.
	sdkIdentifier = "sentry.go.fiber"

	// valuesKey is used as a key to store the Sentry Hub instance on the fasthttp.RequestCtx.
	valuesKey = "sentry"

	// transactionKey is used as a key to store the Sentry transaction on the fasthttp.RequestCtx.
	transactionKey = "sentry_transaction"
)

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

// New returns a handler struct which satisfies Fiber's middleware interface
func New(options Options) fiber.Handler {
	if options.Timeout == 0 {
		options.Timeout = 2 * time.Second
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(ctx *fiber.Ctx) error {
	hub := GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	if client := hub.Client(); client != nil {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	r := convert(ctx)

	transactionName := ctx.Path()
	transactionSource := sentry.SourceURL

	options := []sentry.SpanOption{
		sentry.ContinueTrace(hub, r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(transactionSource),
		sentry.WithSpanOrigin(sentry.SpanOriginFiber),
	}

	transaction := sentry.StartTransaction(
		sentry.SetHubOnContext(ctx.Context(), hub),
		fmt.Sprintf("%s %s", r.Method, transactionName),
		options...,
	)

	defer func() {
		status := ctx.Response().StatusCode()
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	transaction.SetData("http.request.method", r.Method)

	scope := hub.Scope()
	scope.SetRequest(r)
	scope.SetRequestBody(ctx.Request().Body())
	ctx.Locals(valuesKey, hub)
	ctx.Locals(transactionKey, transaction)
	defer h.recoverWithSentry(hub, ctx)

	return ctx.Next()
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, ctx *fiber.Ctx) {
	if err := recover(); err != nil {
		eventID := hub.RecoverWithContext(
			context.WithValue(context.Background(), sentry.RequestContextKey, ctx),
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

// GetHubFromContext retrieves the Hub instance from the *fiber.Ctx.
func GetHubFromContext(ctx *fiber.Ctx) *sentry.Hub {
	if hub, ok := ctx.Locals(valuesKey).(*sentry.Hub); ok {
		return hub
	}
	return nil
}

// SetHubOnContext sets the Hub instance on the *fiber.Ctx.
func SetHubOnContext(ctx *fiber.Ctx, hub *sentry.Hub) {
	ctx.Locals(valuesKey, hub)
}

// GetSpanFromContext retrieves the Span instance from the *fiber.Ctx.
func GetSpanFromContext(ctx *fiber.Ctx) *sentry.Span {
	if span, ok := ctx.Locals(transactionKey).(*sentry.Span); ok {
		return span
	}
	return nil
}

func convert(ctx *fiber.Ctx) *http.Request {
	defer func() {
		if err := recover(); err != nil {
			sentry.Logger.Printf("%v", err)
		}
	}()

	r := new(http.Request)

	r.Method = utils.CopyString(ctx.Method())

	uri := ctx.Request().URI()
	url, err := url.Parse(fmt.Sprintf("%s://%s%s", uri.Scheme(), uri.Host(), uri.Path()))
	if err == nil {
		r.URL = url
		r.URL.RawQuery = string(uri.QueryString())
	}

	host := utils.CopyString(ctx.Hostname())
	r.Host = host

	// Headers
	r.Header = make(http.Header)
	r.Header.Add("Host", host)

	ctx.Request().Header.VisitAll(func(key, value []byte) {
		r.Header.Add(string(key), string(value))
	})

	// Cookies
	ctx.Request().Header.VisitAllCookie(func(key, value []byte) {
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)})
	})

	r.RemoteAddr = ctx.Context().RemoteAddr().String()

	r.Body = io.NopCloser(bytes.NewReader(ctx.Request().Body()))

	return r
}
