package sentrygrpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

func TestUnaryClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		ctx        context.Context
		invoker    grpc.UnaryInvoker
		assertions func(t *testing.T, transport *sentry.MockTransport)
	}{
		"Default behavior, no error": {
			ctx: context.Background(),
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return nil
			},
			assertions: func(t *testing.T, transport *sentry.MockTransport) {
				assert.Empty(t, transport.Events(), "No events should be captured")
			},
		},
		"Metadata propagation": {
			ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("existing", "value")),
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok, "Metadata should be present in the outgoing context")
				assert.Contains(t, md, sentry.SentryTraceHeader, "Metadata should contain Sentry trace header")
				assert.Contains(t, md, sentry.SentryBaggageHeader, "Metadata should contain Sentry baggage header")
				assert.Contains(t, md, "existing", "Metadata should contain key")
				return nil
			},
			assertions: func(t *testing.T, transport *sentry.MockTransport) {},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &sentry.MockTransport{}
			sentry.Init(sentry.ClientOptions{
				Transport: transport,
			})

			interceptor := sentrygrpc.UnaryClientInterceptor()

			// Execute the interceptor
			interceptor(test.ctx, "/test.Service/TestMethod", struct{}{}, struct{}{}, nil, test.invoker)

			sentry.Flush(2 * time.Second)

			// Pass the transport to the assertions to verify captured events.
			test.assertions(t, transport)
		})
	}
}

func TestStreamClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		streamer   grpc.Streamer
		assertions func(t *testing.T, transport *sentry.MockTransport)
		streamDesc *grpc.StreamDesc
	}{
		"Default behavior, no error": {
			streamer: func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, nil
			},
			streamDesc: &grpc.StreamDesc{
				ClientStreams: true,
			},
			assertions: func(t *testing.T, transport *sentry.MockTransport) {
				assert.Empty(t, transport.Events(), "No events should be captured")
			},
		},
		"Metadata propagation": {
			streamer: func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok, "Metadata should be present in the outgoing context")
				assert.Contains(t, md, sentry.SentryTraceHeader, "Metadata should contain Sentry trace header")
				assert.Contains(t, md, sentry.SentryBaggageHeader, "Metadata should contain Sentry baggage header")
				return nil, nil
			},
			streamDesc: &grpc.StreamDesc{
				ClientStreams: true,
			},
			assertions: func(t *testing.T, transport *sentry.MockTransport) {},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Reinitialize the transport for each test to ensure isolation.
			transport := &sentry.MockTransport{}
			sentry.Init(sentry.ClientOptions{
				Transport: transport,
			})

			interceptor := sentrygrpc.StreamClientInterceptor()

			// Execute the interceptor
			clientStream, _ := interceptor(context.Background(), test.streamDesc, nil, "/test.Service/TestMethod", test.streamer)
			sentry.Flush(2 * time.Second)

			assert.Nil(t, clientStream, "ClientStream should be nil in this test scenario")
			// Pass the transport to the assertions to verify captured events.
			test.assertions(t, transport)
		})
	}
}
