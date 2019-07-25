package sentryhttp

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/valyala/fasthttp"
)

type contextKey int

const ContextKey = contextKey(1)
const valuesKey = "sentry"

type Handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to false,
	// as fasthttp doesn't include it's own Recovery handler.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because fasthttp doesn't include it's own `Recovery` handler, it will restart the application,
	// and event won't be delivered otherwise.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a struct that provides Handle method
// that satisfy fasthttp.RequestHandler interface.
func New(options Options) *Handler {
	handler := Handler{
		repanic:         false,
		timeout:         time.Second * 2,
		waitForDelivery: false,
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

	return &handler
}

// Handle wraps fasthttp.RequestHandler and recovers from caught panics.
func (h *Handler) Handle(handler fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		hub := sentry.CurrentHub().Clone()
		hub.Scope().SetRequest(extractRequestData(ctx))
		ctx.SetUserValue(valuesKey, hub)
		defer h.recoverWithSentry(hub, ctx)
		handler(ctx)
	}
}

func (h *Handler) recoverWithSentry(hub *sentry.Hub, ctx *fasthttp.RequestCtx) {
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

// GetHubFromContext retrieves attached *sentry.Hub instance from fasthttp.RequestCtx.
func GetHubFromContext(ctx *fasthttp.RequestCtx) *sentry.Hub {
	hub := ctx.UserValue(valuesKey)
	if hub, ok := hub.(*sentry.Hub); ok {
		return hub
	}
	return nil
}

func extractRequestData(ctx *fasthttp.RequestCtx) sentry.Request {
	defer func() {
		if err := recover(); err != nil {
			sentry.Logger.Printf("%v", err)
		}
	}()

	r := sentry.Request{}

	r.Method = string(ctx.Method())
	uri := ctx.URI()
	r.URL = fmt.Sprintf("%s://%s%s", uri.Scheme(), uri.Host(), uri.Path())

	// Headers
	headers := make(map[string]string)
	ctx.Request.Header.VisitAll(func(key, value []byte) {
		headers[string(key)] = string(value)
	})
	headers["Host"] = string(ctx.Host())
	r.Headers = headers

	// Cookies
	cookies := []string{}
	ctx.Request.Header.VisitAllCookie(func(key, value []byte) {
		cookies = append(cookies, fmt.Sprintf("%s=%s", key, value))
	})
	r.Cookies = strings.Join(cookies, "; ")

	// Env
	if addr, port, err := net.SplitHostPort(ctx.RemoteAddr().String()); err == nil {
		r.Env = map[string]string{"REMOTE_ADDR": addr, "REMOTE_PORT": port}
	}

	// QueryString
	r.QueryString = string(ctx.URI().QueryString())

	// Body
	r.Data = string(ctx.Request.Body())

	return r
}
