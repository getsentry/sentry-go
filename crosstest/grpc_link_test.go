package crosstest

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestGRPCServerLinksManualErrorsLogsMetricsAndPanicsToOTel(t *testing.T) {
	t.Parallel()
	otelCtx, traceID, spanID := fixedOTelContext()

	sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
		const identifier = "grpc"
		baseCtx := f.NewContext(context.Background())
		logger := sentry.NewLogger(baseCtx)
		meter := sentry.NewMeter(baseCtx)
		ctx := metadata.NewIncomingContext(f.NewContext(otelCtx), metadata.Pairs("x-request-id", "req-123"))
		interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{WaitForDelivery: true})

		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
			FullMethod: "/test.TestService/Method",
		}, func(ctx context.Context, _ any) (any, error) {
			sendContextSignals(ctx, identifier, logger, meter)
			return nil, nil
		})
		if status.Code(err) != codes.Internal {
			t.Fatalf("status code = %s, want %s", status.Code(err), codes.Internal)
		}

		f.Flush()
		requireRequestSignalsLinked(t, f.Events(), traceID, spanID, identifier)
	}, otelOpts()...)
}
