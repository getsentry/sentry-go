package sentryfasthttp

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/valyala/fasthttp"
)

// sdkIdentifier is the identifier of the FastHTTP SDK.
const sdkIdentifier = "sentry.go.fasthttp"

type contextKey struct{}
type isolationContextKey struct{}

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
	// Because fasthttp doesn't include it's own Recovery handler, it will restart the application,
	// and event won't be delivered otherwise.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a struct that provides Handle method
// that satisfy fasthttp.RequestHandler interface.
func New(options Options) *Handler {
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return &Handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}
}

// Handle wraps fasthttp.RequestHandler and recovers from caught panics.
func (h *Handler) Handle(handler fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		parentCtx := GetContext(ctx)
		if parentCtx.Value(isolationContextKey{}) == ctx {
			parentCtx = context.Background()
		}
		requestCtx, scope := sentry.WithIsolationScope(parentCtx)

		if client := sentry.GetClient(requestCtx); client.IsEnabled() {
			client.SetSDKIdentifier(sdkIdentifier)
		}

		r := convert(ctx)

		options := []sentry.SpanOption{
			sentry.ContinueTrace(r.Header.Get(sentry.SentryTraceHeader), r.Header.Get(sentry.SentryBaggageHeader)),
			sentry.WithOpName("http.server"),
			sentry.WithTransactionSource(sentry.SourceURL),
			sentry.WithSpanOrigin(sentry.SpanOriginFastHTTP),
		}

		transaction := sentry.StartTransaction(
			requestCtx,
			fmt.Sprintf("%s %s", r.Method, string(ctx.Path())),
			options...,
		)
		requestCtx = context.WithValue(transaction.Context(), isolationContextKey{}, ctx)
		ctx.SetUserValue(contextKey{}, requestCtx)
		defer func() {
			status := ctx.Response.StatusCode()
			transaction.Status = sentry.HTTPtoSpanStatus(status)
			transaction.SetData("http.response.status_code", status)
			transaction.Finish()
		}()

		transaction.SetData("http.request.method", r.Method)
		r = r.WithContext(requestCtx)

		scope.SetRequest(r)
		scope.SetRequestBody(bytes.Clone(ctx.Request.Body()))
		defer h.recoverWithSentry(ctx)

		handler(ctx)
	}
}

func (h *Handler) recoverWithSentry(ctx *fasthttp.RequestCtx) {
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

// GetContext retrieves the request context from fasthttp.RequestCtx.
func GetContext(ctx *fasthttp.RequestCtx) context.Context {
	if requestCtx, ok := ctx.UserValue(contextKey{}).(context.Context); ok {
		return requestCtx
	}
	return context.Background()
}

// SetContext attaches a request context to fasthttp.RequestCtx.
func SetContext(requestCtx context.Context, ctx *fasthttp.RequestCtx) {
	ctx.SetUserValue(contextKey{}, requestCtx)
}

// GetScopeFromContext retrieves the isolation scope from fasthttp.RequestCtx.
func GetScopeFromContext(ctx *fasthttp.RequestCtx) *sentry.Scope {
	return sentry.ScopeFromContext(GetContext(ctx))
}

// GetSpanFromContext retrieves the active span from fasthttp.RequestCtx.
func GetSpanFromContext(ctx *fasthttp.RequestCtx) *sentry.Span {
	return sentry.SpanFromContext(GetContext(ctx))
}

func convert(ctx *fasthttp.RequestCtx) *http.Request {
	defer func() {
		if err := recover(); err != nil {
			debuglog.Printf("%v", err)
		}
	}()

	r := new(http.Request)

	r.Method = string(ctx.Method())

	uri := ctx.URI()
	r.URL = &url.URL{Path: string(uri.Path())}
	r.URL.RawQuery = string(uri.QueryString())

	if parsedURL, err := url.Parse(fmt.Sprintf("%s://%s%s", uri.Scheme(), uri.Host(), uri.Path())); err == nil {
		r.URL = parsedURL
		r.URL.RawQuery = string(uri.QueryString())
	}

	host := string(ctx.Host())
	r.Host = host

	// Headers
	r.Header = make(http.Header)
	r.Header.Add("Host", host)
	for key, value := range ctx.Request.Header.All() {
		r.Header.Add(string(key), string(value))
	}

	// Cookies
	for key, value := range ctx.Request.Header.Cookies() {
		r.AddCookie(&http.Cookie{Name: string(key), Value: string(value)})
	}

	r.RemoteAddr = ctx.RemoteAddr().String()

	return r
}
