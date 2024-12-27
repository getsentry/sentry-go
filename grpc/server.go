package sentrygrpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	sdkIdentifier              = "sentry.go.grpc"
	defaultServerOperationName = "grpc.server"
)

type ServerOptions struct {
	// Repanic determines whether the application should re-panic after recovery.
	Repanic bool

	// WaitForDelivery determines if the interceptor should block until events are sent to Sentry.
	WaitForDelivery bool

	// Timeout sets the maximum duration for Sentry event delivery.
	Timeout time.Duration

	// ReportOn defines the conditions under which errors are reported to Sentry.
	ReportOn func(error) bool

	// CaptureRequestBody determines whether to capture and send request bodies to Sentry.
	CaptureRequestBody bool

	// OperationName overrides the default operation name (grpc.server).
	OperationName string
}

func (o *ServerOptions) SetDefaults() {
	if o.ReportOn == nil {
		o.ReportOn = func(err error) bool {
			return true
		}
	}

	if o.Timeout == 0 {
		o.Timeout = sentry.DefaultFlushTimeout
	}

	if o.OperationName == "" {
		o.OperationName = defaultServerOperationName
	}
}

func recoverWithSentry(ctx context.Context, hub *sentry.Hub, o ServerOptions) {
	if r := recover(); r != nil {
		eventID := hub.RecoverWithContext(ctx, r)

		if eventID != nil && o.WaitForDelivery {
			hub.Flush(o.Timeout)
		}

		if o.Repanic {
			panic(r)
		}
	}
}

func reportErrorToSentry(hub *sentry.Hub, err error, methodName string, req any, md map[string]string) {
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetExtras(map[string]any{
			"grpc.method": methodName,
			"grpc.error":  err.Error(),
		})

		if req != nil {
			scope.SetExtra("request", req)
		}

		if len(md) > 0 {
			scope.SetExtra("metadata", md)
		}

		defer hub.CaptureException(err)

		statusErr, ok := status.FromError(err)
		if !ok {
			return
		}

		for _, detail := range statusErr.Details() {
			debugInfo, ok := detail.(*errdetails.DebugInfo)
			if !ok {
				continue
			}
			hub.AddBreadcrumb(&sentry.Breadcrumb{
				Type:      "debug",
				Category:  "grpc.server",
				Message:   debugInfo.Detail,
				Data:      map[string]any{"stackTrace": strings.Join(debugInfo.StackEntries, "\n")},
				Level:     sentry.LevelError,
				Timestamp: time.Now(),
			}, nil)
		}
	})
}

func UnaryServerInterceptor(opts ServerOptions) grpc.UnaryServerInterceptor {
	opts.SetDefaults()

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}

		if client := hub.Client(); client != nil {
			client.SetSDKIdentifier(sdkIdentifier)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		var sentryTraceHeader, sentryBaggageHeader string
		data := make(map[string]string)
		if ok {
			sentryTraceHeader = getFirstHeader(md, sentry.SentryTraceHeader)
			sentryBaggageHeader = getFirstHeader(md, sentry.SentryBaggageHeader)

			for k, v := range md {
				data[k] = strings.Join(v, ",")
			}
		}

		options := []sentry.SpanOption{
			sentry.ContinueTrace(hub, sentryTraceHeader, sentryBaggageHeader),
			sentry.WithOpName(opts.OperationName),
			sentry.WithDescription(info.FullMethod),
			sentry.WithTransactionSource(sentry.SourceURL),
		}

		transaction := sentry.StartTransaction(
			sentry.SetHubOnContext(ctx, hub),
			fmt.Sprintf("%s %s", "UnaryServerInterceptor", info.FullMethod),
			options...,
		)

		transaction.SetData("http.request.method", info.FullMethod)

		ctx = transaction.Context()
		defer transaction.Finish()

		if opts.CaptureRequestBody {
			// Marshal from proto.Message to bytes? Slow?
			// hub.Scope().SetRequestBody(req)
		}

		defer recoverWithSentry(ctx, hub, opts)

		resp, err := handler(ctx, req)
		if err != nil && opts.ReportOn(err) {
			reportErrorToSentry(hub, err, info.FullMethod, req, data)

			transaction.Sampled = sentry.SampledTrue
		}

		statusCode := status.Code(err)
		transaction.Status = toSpanStatus(statusCode)
		transaction.SetData("http.response.status_code", statusCode.String())

		return resp, err
	}
}

// StreamServerInterceptor provides Sentry integration for streaming gRPC calls.
func StreamServerInterceptor(opts ServerOptions) grpc.StreamServerInterceptor {
	opts.SetDefaults()
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		ctx := ss.Context()
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
		}

		if client := hub.Client(); client != nil {
			client.SetSDKIdentifier(sdkIdentifier)
		}

		md, ok := metadata.FromIncomingContext(ctx)
		var sentryTraceHeader, sentryBaggageHeader string
		data := make(map[string]string)
		if ok {
			sentryTraceHeader = getFirstHeader(md, sentry.SentryTraceHeader)
			sentryBaggageHeader = getFirstHeader(md, sentry.SentryBaggageHeader)

			for k, v := range md {
				data[k] = strings.Join(v, ",")
			}
		}

		options := []sentry.SpanOption{
			sentry.ContinueTrace(hub, sentryTraceHeader, sentryBaggageHeader),
			sentry.WithOpName(opts.OperationName),
			sentry.WithDescription(info.FullMethod),
			sentry.WithTransactionSource(sentry.SourceURL),
		}

		transaction := sentry.StartTransaction(
			sentry.SetHubOnContext(ctx, hub),
			fmt.Sprintf("%s %s", "StreamServerInterceptor", info.FullMethod),
			options...,
		)

		transaction.SetData("grpc.method", info.FullMethod)
		ctx = transaction.Context()
		defer transaction.Finish()

		stream := wrapServerStream(ss, ctx)

		defer recoverWithSentry(ctx, hub, opts)

		err := handler(srv, stream)
		if err != nil && opts.ReportOn(err) {
			reportErrorToSentry(hub, err, info.FullMethod, nil, data)

			transaction.Sampled = sentry.SampledTrue
		}

		statusCode := status.Code(err)
		transaction.Status = toSpanStatus(statusCode)
		transaction.SetData("grpc.status", statusCode.String())

		return err
	}
}

func getFirstHeader(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}

// wrapServerStream wraps a grpc.ServerStream, allowing you to inject a custom context.
func wrapServerStream(ss grpc.ServerStream, ctx context.Context) grpc.ServerStream {
	return &wrappedServerStream{ServerStream: ss, ctx: ctx}
}

// wrappedServerStream is a wrapper around grpc.ServerStream that overrides the Context method.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the custom context for the stream.
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

var codeToSpanStatus = map[codes.Code]sentry.SpanStatus{
	codes.OK:                 sentry.SpanStatusOK,
	codes.Canceled:           sentry.SpanStatusCanceled,
	codes.Unknown:            sentry.SpanStatusUnknown,
	codes.InvalidArgument:    sentry.SpanStatusInvalidArgument,
	codes.DeadlineExceeded:   sentry.SpanStatusDeadlineExceeded,
	codes.NotFound:           sentry.SpanStatusNotFound,
	codes.AlreadyExists:      sentry.SpanStatusAlreadyExists,
	codes.PermissionDenied:   sentry.SpanStatusPermissionDenied,
	codes.ResourceExhausted:  sentry.SpanStatusResourceExhausted,
	codes.FailedPrecondition: sentry.SpanStatusFailedPrecondition,
	codes.Aborted:            sentry.SpanStatusAborted,
	codes.OutOfRange:         sentry.SpanStatusOutOfRange,
	codes.Unimplemented:      sentry.SpanStatusUnimplemented,
	codes.Internal:           sentry.SpanStatusInternalError,
	codes.Unavailable:        sentry.SpanStatusUnavailable,
	codes.DataLoss:           sentry.SpanStatusDataLoss,
	codes.Unauthenticated:    sentry.SpanStatusUnauthenticated,
}

func toSpanStatus(code codes.Code) sentry.SpanStatus {
	if status, ok := codeToSpanStatus[code]; ok {
		return status
	}
	return sentry.SpanStatusUndefined
}
