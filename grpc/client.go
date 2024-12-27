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

type ClientOptions struct {
	// ReportOn defines the conditions under which errors are reported to Sentry.
	ReportOn func(error) bool

	// OperationName overrides the default operation name (grpc.client).
	OperationName string
}

func (o *ClientOptions) SetDefaults() {
	if o.ReportOn == nil {
		o.ReportOn = func(err error) bool {
			return true
		}
	}
	if o.OperationName == "" {
		o.OperationName = defaultClientOperationName
	}
}

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

func getOrCreateHub(ctx context.Context) (*sentry.Hub, context.Context) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
		ctx = sentry.SetHubOnContext(ctx, hub)
	}
	return hub, ctx
}

func UnaryClientInterceptor(o ClientOptions) grpc.UnaryClientInterceptor {
	o.SetDefaults()
	return func(ctx context.Context,
		method string,
		req, reply interface{},
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		callOpts ...grpc.CallOption) error {

		hub, ctx := getOrCreateHub(ctx)

		span := sentry.StartSpan(ctx, o.OperationName, sentry.WithDescription(method))
		span.SetData("grpc.request.method", method)
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)
		defer span.Finish()

		err := invoker(ctx, method, req, reply, cc, callOpts...)

		if err != nil && o.ReportOn(err) {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("grpc.method", method)
				scope.SetContext("request", map[string]any{
					"method":  method,
					"request": req,
				})
				hub.CaptureException(err)
			})
		}

		return err
	}
}

func StreamClientInterceptor(o ClientOptions) grpc.StreamClientInterceptor {
	o.SetDefaults()
	return func(ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		callOpts ...grpc.CallOption) (grpc.ClientStream, error) {

		hub, ctx := getOrCreateHub(ctx)

		span := sentry.StartSpan(ctx, o.OperationName, sentry.WithDescription(method))
		span.SetData("grpc.request.method", method)
		ctx = span.Context()

		ctx = createOrUpdateMetadata(ctx, span)
		defer span.Finish()

		clientStream, err := streamer(ctx, desc, cc, method, callOpts...)

		if err != nil && o.ReportOn(err) {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("grpc.method", method)
				hub.CaptureException(err)
			})
		}

		return clientStream, err
	}
}
