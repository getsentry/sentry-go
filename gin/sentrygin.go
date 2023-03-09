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
	valuesKey      = "sentry"
	traceValuesKey = "sentry-gin"

	// https://develop.sentry.dev/sdk/performance/#header-sentry-trace
	traceHeader = "sentry-trace"

	// https://develop.sentry.dev/sdk/performance/dynamic-sampling-context/#baggage
	baggageHeader = "baggage"
)

// Convert HTTP Status to [Sentry status], rewrite from Sentry Python SDK's [tracing.py]
//
// [Sentry status]: https://develop.sentry.dev/sdk/event-payloads/properties/status/
// [tracing.py]: https://github.com/getsentry/sentry-python/blob/1.12.0/sentry_sdk/tracing.py#L436-L467
func FromHTTPStatusToSentryStatus(code int) sentry.SpanStatus {
	if code < http.StatusBadRequest {
		return sentry.SpanStatusOK
	}
	if http.StatusBadRequest <= code && code < http.StatusInternalServerError {
		switch code {
		case http.StatusForbidden:
			return sentry.SpanStatusPermissionDenied
		case http.StatusNotFound:
			return sentry.SpanStatusNotFound
		case http.StatusTooManyRequests:
			return sentry.SpanStatusResourceExhausted
		case http.StatusRequestEntityTooLarge:
			return sentry.SpanStatusFailedPrecondition
		case http.StatusUnauthorized:
			return sentry.SpanStatusUnauthenticated
		case http.StatusConflict:
			return sentry.SpanStatusAlreadyExists
		default:
			return sentry.SpanStatusInvalidArgument
		}
	}
	if http.StatusInternalServerError <= code && code < 600 {
		switch code {
		case http.StatusGatewayTimeout:
			return sentry.SpanStatusDeadlineExceeded
		case http.StatusNotImplemented:
			return sentry.SpanStatusUnimplemented
		case http.StatusServiceUnavailable:
			return sentry.SpanStatusUnavailable
		default:
			return sentry.SpanStatusInternalError
		}
	}
	return sentry.SpanStatusUnknown
}

type handler struct {
	repanic         bool
	waitForDelivery bool
	timeout         time.Duration

	getTraceIDFromRequest func(*gin.Context) string
	getBaggageFromRequest func(*gin.Context) string
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
	// Extract Sentry trace id from request, by default use `sentry-trace` from header
	GetTraceIDFromRequest func(*gin.Context) string
	// Extract Sentry baggage from request, by default use `baggage` from header
	GetBaggageFromRequest func(*gin.Context) string
}

func extractTraceFromRequest(ctx *gin.Context) string {
	return ctx.GetHeader(traceHeader)
}

func extractBaggageFromRequest(ctx *gin.Context) string {
	return ctx.GetHeader(baggageHeader)
}

// New returns a function that satisfies gin.HandlerFunc interface
// It can be used with Use() methods.
func New(options Options) gin.HandlerFunc {
	timeout := options.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	if options.GetTraceIDFromRequest == nil {
		options.GetTraceIDFromRequest = extractTraceFromRequest
	}
	if options.GetBaggageFromRequest == nil {
		options.GetBaggageFromRequest = extractBaggageFromRequest
	}
	return (&handler{
		repanic:               options.Repanic,
		timeout:               timeout,
		waitForDelivery:       options.WaitForDelivery,
		getTraceIDFromRequest: options.GetTraceIDFromRequest,
		getBaggageFromRequest: options.GetBaggageFromRequest,
	}).handle
}

func (h *handler) handle(ctx *gin.Context) {
	hub := sentry.GetHubFromContext(ctx.Request.Context())
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}
	hub.Scope().SetRequest(ctx.Request)

	transaction := sentry.StartTransaction(
		ctx, fmt.Sprintf("%v %v", ctx.Request.Method, ctx.FullPath()),
		sentry.ContinueFromHeaders(h.getTraceIDFromRequest(ctx), h.getBaggageFromRequest(ctx)),
	)
	ctx.Writer.Header().Set(traceHeader, transaction.ToSentryTrace())
	ctx.Writer.Header().Set(baggageHeader, transaction.ToBaggage())
	ctx.Set(traceValuesKey, transaction)

	ctx.Set(valuesKey, hub)
	defer h.recoverWithSentry(hub, ctx.Request)
	ctx.Next()

	transaction.Status = FromHTTPStatusToSentryStatus(ctx.Writer.Status())
	transaction.Finish()
}

func (h *handler) recoverWithSentry(hub *sentry.Hub, r *http.Request) {
	if err := recover(); err != nil {
		if !isBrokenPipeError(err) {
			eventID := hub.RecoverWithContext(
				context.WithValue(r.Context(), sentry.RequestContextKey, r),
				err,
			)
			if eventID != nil && h.waitForDelivery {
				hub.Flush(h.timeout)
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

// GetHubFromContext retrieves attached *sentry.Hub instance from gin.Context.
func GetHubFromContext(ctx *gin.Context) *sentry.Hub {
	if hub, ok := ctx.Get(valuesKey); ok {
		if hub, ok := hub.(*sentry.Hub); ok {
			return hub
		}
	}
	return nil
}

// StartSpanFromGinContext start a new *sentry.Span from gin.Context.
func StartSpanFromGinContext(ctx *gin.Context, op string) *sentry.Span {
	span, ok := ctx.Value(traceValuesKey).(*sentry.Span)
	if ok && span != nil {
		return span.StartChild(op)
	}
	return sentry.StartSpan(ctx, op)
}
