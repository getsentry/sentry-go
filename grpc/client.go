// SPDX-License-Identifier: Apache-2.0
// Part of this code is derived from [github.com/johnbellone/grpc-middleware-sentry], licensed under the Apache 2.0 License.

package sentrygrpc

import (
	"context"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const defaultClientOperationName = "grpc.client"

func createOrUpdateMetadata(ctx context.Context, span *sentry.Span) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if ok {
		md = md.Copy()
		md.Append(sentry.SentryTraceHeader, span.ToSentryTrace())
		md.Append(sentry.SentryBaggageHeader, span.ToBaggage())
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
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		callOpts ...grpc.CallOption) error {
		span := sentry.StartSpan(ctx, defaultClientOperationName, sentry.WithDescription(method))
		span.SetData("grpc.request.method", method)
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)
		defer span.Finish()

		return invoker(ctx, method, req, reply, cc, callOpts...)
	}
}

func StreamClientInterceptor() grpc.StreamClientInterceptor {
	return func(ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		callOpts ...grpc.CallOption) (grpc.ClientStream, error) {
		span := sentry.StartSpan(ctx, defaultClientOperationName, sentry.WithDescription(method))
		span.SetData("grpc.request.method", method)
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)
		defer span.Finish()

		return streamer(ctx, desc, cc, method, callOpts...)
	}
}
