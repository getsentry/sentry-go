// SPDX-License-Identifier: Apache-2.0
// Part of this code is derived from [github.com/johnbellone/grpc-middleware-sentry], licensed under the Apache 2.0 License.

package sentrygrpc

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const defaultClientOperationName = "rpc.client"

func hubFromClientContext(ctx context.Context) context.Context {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
		ctx = sentry.SetHubOnContext(ctx, hub)
	}

	if client := hub.Client(); client != nil {
		client.SetSDKIdentifier(sdkIdentifier)
	}

	return ctx
}

func createOrUpdateMetadata(ctx context.Context, span *sentry.Span) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
		md.Set(sentry.SentryTraceHeader, span.ToSentryTrace())
		md.Set(sentry.SentryBaggageHeader, span.ToBaggage())
		return metadata.NewOutgoingContext(ctx, md)
	}

	md = metadata.Pairs(
		sentry.SentryTraceHeader, span.ToSentryTrace(),
		sentry.SentryBaggageHeader, span.ToBaggage(),
	)

	return metadata.NewOutgoingContext(ctx, md)
}

func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		callOpts ...grpc.CallOption) error {
		ctx = hubFromClientContext(ctx)
		name, service, rpcMethod := parseGRPCMethod(method)
		span := sentry.StartSpan(
			ctx,
			defaultClientOperationName,
			sentry.WithTransactionName(name),
			sentry.WithDescription(name),
			sentry.WithSpanOrigin(sentry.SpanOriginGrpc),
		)
		if service != "" {
			span.SetData("rpc.service", service)
		}
		if rpcMethod != "" {
			span.SetData("rpc.method", rpcMethod)
		}
		span.SetData("rpc.system", "grpc")
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)
		defer span.Finish()

		err := invoker(ctx, method, req, reply, cc, callOpts...)
		span.Status = toSpanStatus(status.Code(err))
		span.SetData("rpc.grpc.status_code", int(status.Code(err)))
		return err
	}
}

func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		callOpts ...grpc.CallOption) (grpc.ClientStream, error) {
		ctx = hubFromClientContext(ctx)
		name, service, rpcMethod := parseGRPCMethod(method)
		span := sentry.StartSpan(
			ctx,
			defaultClientOperationName,
			sentry.WithTransactionName(name),
			sentry.WithDescription(name),
			sentry.WithSpanOrigin(sentry.SpanOriginGrpc),
		)
		if service != "" {
			span.SetData("rpc.service", service)
		}
		if rpcMethod != "" {
			span.SetData("rpc.method", rpcMethod)
		}
		span.SetData("rpc.system", "grpc")
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)

		stream, err := streamer(ctx, desc, cc, method, callOpts...)
		if err != nil {
			span.Status = toSpanStatus(status.Code(err))
			span.SetData("rpc.grpc.status_code", int(status.Code(err)))
			span.Finish()
			return nil, err
		}
		if stream == nil {
			nilErr := status.Error(codes.Internal, "streamer returned nil stream without error")
			span.Status = toSpanStatus(codes.Internal)
			span.SetData("rpc.grpc.status_code", int(codes.Internal))
			span.Finish()
			return nil, nilErr
		}

		return &sentryClientStream{ClientStream: stream, span: span}, nil
	}
}

type sentryClientStream struct {
	grpc.ClientStream
	span       *sentry.Span
	finishOnce sync.Once
}

func (s *sentryClientStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil {
		s.finish(err)
	}
	return md, err
}

func (s *sentryClientStream) CloseSend() error {
	err := s.ClientStream.CloseSend()
	if err != nil {
		s.finish(err)
	}
	return err
}

func (s *sentryClientStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil {
		s.finish(err)
	}
	return err
}

func (s *sentryClientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	if err != nil {
		if errors.Is(err, io.EOF) {
			s.finish(nil)
		} else {
			s.finish(err)
		}
	}
	return err
}

func (s *sentryClientStream) finish(err error) {
	s.finishOnce.Do(func() {
		s.span.Status = toSpanStatus(status.Code(err))
		if err == nil {
			s.span.SetData("rpc.grpc.status_code", int(codes.OK))
		} else {
			s.span.SetData("rpc.grpc.status_code", int(status.Code(err)))
		}
		s.span.Finish()
	})
}
