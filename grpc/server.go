package sentrygrpc

import (
	"context"

	"github.com/getsentry/sentry-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// UnaryServerInterceptor is a grpc interceptor that reports errors and panics
// to sentry. It also sets *sentry.Hub to context.
func UnaryServerInterceptor(options ...Option) grpc.UnaryServerInterceptor {
	opts := buildOptions(options...)

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (resp interface{}, err error) {
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, hub)
		}

		defer func() {
			if r := recover(); r != nil {
				hub.RecoverWithContext(ctx, r)

				if opts.repanic {
					panic(r)
				}

				err = status.Errorf(codes.Internal, "%s", r)
			}
		}()

		resp, err = handler(ctx, req)

		if opts.reportOn(err) {
			hub.CaptureException(err)
		}

		return resp, err
	}
}
