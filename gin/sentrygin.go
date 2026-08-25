package sentrygin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
)

const (
	// sdkIdentifier is the identifier of the Gin SDK.
	sdkIdentifier = "sentry.go.gin"
)

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration
}

type Options struct {
	// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to true,
	// as gin.Default includes it's own Recovery middleware what handles http responses.
	Repanic bool
	// WaitForDelivery configures whether you want to block the request before moving forward with the response.
	// Because Gin's default Recovery handler doesn't restart the application,
	// it's safe to either skip this option or set it to false.
	WaitForDelivery bool
	// Timeout for the event delivery requests.
	Timeout time.Duration
}

// New returns a function that satisfies gin.HandlerFunc interface
// It can be used with Use() methods.
func New(options Options) gin.HandlerFunc {
	if options.Timeout == 0 {
		options.Timeout = sentry.DefaultFlushTimeout
	}

	return (&handler{
		repanic:         options.Repanic,
		timeout:         options.Timeout,
		waitForDelivery: options.WaitForDelivery,
	}).handle
}

func (h *handler) handle(c *gin.Context) {
	ctx, scope := sentry.WithIsolationScope(c.Request.Context())

	if client := sentry.GetClient(ctx); client.IsEnabled() {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	transactionName := c.Request.URL.Path
	transactionSource := sentry.SourceURL

	if fp := c.FullPath(); fp != "" {
		transactionName = fp
		transactionSource = sentry.SourceRoute
	}

	options := []sentry.SpanOption{
		sentry.ContinueTrace(c.GetHeader(sentry.SentryTraceHeader), c.GetHeader(sentry.SentryBaggageHeader)),
		sentry.WithOpName("http.server"),
		sentry.WithTransactionSource(transactionSource),
		sentry.WithSpanOrigin(sentry.SpanOriginGin),
	}

	transaction := sentry.StartTransaction(
		ctx,
		fmt.Sprintf("%s %s", c.Request.Method, transactionName),
		options...,
	)

	transaction.SetData("http.request.method", c.Request.Method)

	defer func() {
		status := c.Writer.Status()
		transaction.Status = sentry.HTTPtoSpanStatus(status)
		transaction.SetData("http.response.status_code", status)
		transaction.Finish()
	}()

	c.Request = c.Request.WithContext(transaction.Context())
	scope.SetRequest(c.Request)
	defer h.recoverWithSentry(c.Request)

	c.Next()
}

func (h *handler) recoverWithSentry(r *http.Request) {
	if err := recover(); err != nil {
		if !isBrokenPipeError(err) {
			ctx := context.WithValue(r.Context(), sentry.RequestContextKey, r)
			eventID := sentry.CapturePanic(ctx, err)
			if eventID != nil && h.waitForDelivery {
				sentry.GetClient(ctx).Flush(h.timeout)
			}
		}
		if h.repanic {
			panic(err)
		}
	}
}

// Check for a broken connection, as this is what Gin does already.
func isBrokenPipeError(err interface{}) bool {
	if netErr, ok := err.(*net.OpError); ok {
		if sysErr, ok := netErr.Err.(*os.SyscallError); ok {
			if strings.Contains(strings.ToLower(sysErr.Error()), "broken pipe") ||
				strings.Contains(strings.ToLower(sysErr.Error()), "connection reset by peer") {
				return true
			}
		}
	}
	return false
}

// GetScopeFromContext retrieves the isolation scope from gin.Context.
func GetScopeFromContext(ctx *gin.Context) *sentry.Scope {
	return sentry.ScopeFromContext(ctx.Request.Context())
}

// GetSpanFromContext retrieves the active span from gin.Context.
// If there is no span on gin.Context, it will return nil.
func GetSpanFromContext(ctx *gin.Context) *sentry.Span {
	return sentry.SpanFromContext(ctx.Request.Context())
}
