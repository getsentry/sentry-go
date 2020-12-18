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
func UnaryServerInterceptor(opts UnaryServerInterceptorOptions) grpc.UnaryServerInterceptor {
	if opts.ReportOn == nil {
		opts.ReportOn = ReportAlways
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (_ interface{}, err error) {
		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub().Clone()
			ctx = sentry.SetHubOnContext(ctx, hub)
		}

		defer func() {
			if r := recover(); r != nil {
				hub.RecoverWithContext(ctx, r)

				if opts.Repanic {
					panic(r)
				}

				err = status.Errorf(codes.Internal, "%s", r)
			}
		}()

		resp, err := handler(ctx, req)

		if opts.ReportOn(err) {
			hub.CaptureException(err)
		}

		return resp, err
	}
}

// UnaryServerInterceptor configure UnaryServerInterceptor.
type UnaryServerInterceptorOptions struct {
	// Repanic configures whether to panic again after recovering from a
	// panic. Use this option if you have other panic handlers.
	Repanic bool

	// ReportOn configures whether to report an error. Defaults to
	// ReportAlways.
	ReportOn ReportOn
}

// ReportOn decides error should be reported to sentry.
type ReportOn func(error) bool

// ReportAlways returns true if err is non-nil.
func ReportAlways(err error) bool {
	return err != nil
}

// ReportOnCodes returns true if error code matches on of the given codes.
func ReportOnCodes(cc ...codes.Code) ReportOn {
	return func(err error) bool {
		c := status.Code(err)
		for i := range cc {
			if c == cc[i] {
				return true
			}
		}

		return false
	}
}
