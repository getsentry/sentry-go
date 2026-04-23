// SPDX-License-Identifier: Apache-2.0
// Part of this code is derived from [github.com/johnbellone/grpc-middleware-sentry], licensed under the Apache 2.0 License.

package sentrygrpc

import (
	"context"
	"errors"
	"io"
	"strings"
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
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(sentry.SentryTraceHeader, span.ToSentryTrace())

	existingBaggage := strings.Join(md.Get(sentry.SentryBaggageHeader), ",")
	mergedBaggage, err := sentry.MergeBaggage(existingBaggage, span.ToBaggage())
	if err == nil {
		md.Set(sentry.SentryBaggageHeader, mergedBaggage)
	}

	return metadata.NewOutgoingContext(ctx, md)
}

func finishSpan(span *sentry.Span, err error) {
	setRPCStatus(span, err)
	span.Finish()
}

func startClientSpan(ctx context.Context, method string) (context.Context, *sentry.Span) {
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

	ctx = createOrUpdateMetadata(span.Context(), span)
	return ctx, span
}

func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		callOpts ...grpc.CallOption) (err error) {
		ctx, span := startClientSpan(ctx, method)
		defer func() {
			finishSpan(span, err)
		}()

		err = invoker(ctx, method, req, reply, cc, callOpts...)
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
		ctx, span := startClientSpan(ctx, method)

		stream, err := streamer(ctx, desc, cc, method, callOpts...)
		if err != nil {
			finishSpan(span, err)
			return nil, err
		}
		if stream == nil {
			nilErr := status.Error(codes.Internal, "streamer returned nil stream without error")
			finishSpan(span, nilErr)
			return nil, nilErr
		}

		wrappedStream := &sentryClientStream{
			ClientStream:  stream,
			serverStreams: desc != nil && desc.ServerStreams,
			span:          span,
		}
		wrappedStream.stopMonitor = context.AfterFunc(ctx, func() {
			wrappedStream.finish(ctx.Err())
		})
		return wrappedStream, nil
	}
}

type sentryClientStream struct {
	grpc.ClientStream
	serverStreams bool
	span          *sentry.Span
	stopMonitor   func() bool
	finishOnce    sync.Once
}

func (s *sentryClientStream) Header() (metadata.MD, error) {
	md, err := s.ClientStream.Header()
	if err != nil {
		s.finish(err)
	}
	return md, err
}
func (s *sentryClientStream) SendMsg(m any) error {
	err := s.ClientStream.SendMsg(m)
	if err != nil && !errors.Is(err, io.EOF) {
		s.finish(err)
	}
	return err
}

func (s *sentryClientStream) RecvMsg(m any) error {
	err := s.ClientStream.RecvMsg(m)
	switch {
	case err == nil && !s.serverStreams:
		s.finish(nil)
	case errors.Is(err, io.EOF):
		s.finish(nil)
	case err != nil:
		s.finish(err)
	}
	return err
}

func (s *sentryClientStream) finish(err error) {
	s.finishOnce.Do(func() {
		if s.stopMonitor != nil {
			s.stopMonitor()
		}
		finishSpan(s.span, err)
	})
}
