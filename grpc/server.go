// SPDX-License-Identifier: Apache-2.0
// Part of this code is derived from [github.com/johnbellone/grpc-middleware-sentry], licensed under the Apache 2.0 License.

package sentrygrpc

import (
	"context"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	sdkIdentifier              = "sentry.go.grpc"
	defaultServerOperationName = "rpc.server"
	internalServerErrorMessage = "internal server error"
)

type ServerOptions struct {
	// Repanic determines whether the application should re-panic after recovery.
	Repanic bool

	// WaitForDelivery determines if the interceptor should block until events are sent to Sentry.
	WaitForDelivery bool

	// Timeout sets the maximum duration for Sentry event delivery.
	Timeout time.Duration
}

func (o *ServerOptions) setDefaults() {
	if o.Timeout == 0 {
		o.Timeout = sentry.DefaultFlushTimeout
	}
}

func recoverWithSentry(ctx context.Context, hub *sentry.Hub, o ServerOptions, onRecover func()) {
	if r := recover(); r != nil {
		eventID := hub.RecoverWithContext(ctx, r)

		if onRecover != nil {
			onRecover()
		}

		if eventID != nil && o.WaitForDelivery {
			hub.Flush(o.Timeout)
		}

		if o.Repanic {
			panic(r)
		}
	}
}

func hubFromServerContext(ctx context.Context) *sentry.Hub {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	if client := hub.Client(); client != nil {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	return hub
}

func traceHeadersFromContext(ctx context.Context) (metadata.MD, string, string) {
	md, _ := metadata.FromIncomingContext(ctx)
	return md, getFirstHeader(md, sentry.SentryTraceHeader), getFirstHeader(md, sentry.SentryBaggageHeader)
}

func startServerTransaction(ctx context.Context, fullMethod string) (context.Context, *sentry.Hub, *sentry.Span) {
	hub := hubFromServerContext(ctx)
	md, sentryTraceHeader, sentryBaggageHeader := traceHeadersFromContext(ctx)
	name, service, method := parseGRPCMethod(fullMethod)

	setScopeMetadata(hub, name, md)

	transaction := sentry.StartTransaction(
		sentry.SetHubOnContext(ctx, hub),
		name,
		sentry.ContinueTrace(hub, sentryTraceHeader, sentryBaggageHeader),
		sentry.WithOpName(defaultServerOperationName),
		sentry.WithDescription(name),
		sentry.WithTransactionSource(sentry.SourceRoute),
		sentry.WithSpanOrigin(sentry.SpanOriginGrpc),
	)
	if service != "" {
		transaction.SetData("rpc.service", service)
	}
	if method != "" {
		transaction.SetData("rpc.method", method)
	}
	transaction.SetData("rpc.system", "grpc")

	return transaction.Context(), hub, transaction
}

func setRPCStatus(span *sentry.Span, err error) {
	code := grpcStatusCode(err)
	span.Status = toSpanStatus(code)
	span.SetData("rpc.grpc.status_code", int(code))
}

func grpcStatusCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}

	if s, ok := status.FromError(err); ok {
		return s.Code()
	}

	return status.FromContextError(err).Code()
}

func UnaryServerInterceptor(opts ServerOptions) grpc.UnaryServerInterceptor {
	opts.setDefaults()

	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		ctx, hub, transaction := startServerTransaction(ctx, info.FullMethod)
		defer func() {
			transaction.Finish()
			if opts.WaitForDelivery {
				hub.Flush(opts.Timeout)
			}
		}()

		defer recoverWithSentry(ctx, hub, opts, func() {
			err = status.Error(codes.Internal, internalServerErrorMessage)
			setRPCStatus(transaction, err)
		})

		resp, err = handler(ctx, req)
		setRPCStatus(transaction, err)

		return resp, err
	}
}

// StreamServerInterceptor provides Sentry integration for streaming gRPC calls.
func StreamServerInterceptor(opts ServerOptions) grpc.StreamServerInterceptor {
	opts.setDefaults()
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		ctx, hub, transaction := startServerTransaction(ss.Context(), info.FullMethod)
		defer func() {
			transaction.Finish()
			if opts.WaitForDelivery {
				hub.Flush(opts.Timeout)
			}
		}()

		stream := wrapServerStream(ss, ctx)

		defer recoverWithSentry(ctx, hub, opts, func() {
			err = status.Error(codes.Internal, internalServerErrorMessage)
			setRPCStatus(transaction, err)
		})

		err = handler(srv, stream)
		setRPCStatus(transaction, err)

		return err
	}
}

func getFirstHeader(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}

func setScopeMetadata(hub *sentry.Hub, method string, md metadata.MD) {
	hub.ConfigureScope(func(scope *sentry.Scope) {
		scope.SetContext("grpc", sentry.Context{
			"method":   method,
			"metadata": metadataToContext(md),
		})
	})
}

func metadataToContext(md metadata.MD) map[string]any {
	if len(md) == 0 {
		return nil
	}

	ctx := make(map[string]any, len(md))
	for key, values := range md {
		if sentry.IsSensitiveHeader(key) {
			continue
		}

		if len(values) == 0 {
			continue
		}

		if len(values) == 1 {
			ctx[key] = values[0]
			continue
		}

		copied := make([]string, len(values))
		copy(copied, values)
		ctx[key] = copied
	}

	if len(ctx) == 0 {
		return nil
	}

	return ctx
}

// parseGRPCMethod parses a gRPC full method name and returns the span name, service, and method components.
//
// It expects the format "/service/method" and parsing is compatible with:
// https://github.com/grpc/grpc-go/blob/v1.79.3/internal/grpcutil/method.go#L28
//
// Returns the original string as name and empty service/method if the format is invalid.
func parseGRPCMethod(fullMethod string) (name, service, method string) {
	if !strings.HasPrefix(fullMethod, "/") {
		return fullMethod, "", ""
	}
	name = fullMethod[1:]
	pos := strings.Index(name, "/")
	if pos < 0 {
		return name, "", ""
	}
	return name, name[:pos], name[pos+1:]
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
	if spanStatus, ok := codeToSpanStatus[code]; ok {
		return spanStatus
	}
	return sentry.SpanStatusUndefined
}
