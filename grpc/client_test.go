package sentrygrpc_test

import (
	"context"
	"io"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/internal/testutils"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockClientStream implements grpc.ClientStream for testing.
type mockClientStream struct {
	headerFn    func() (metadata.MD, error)
	closeSendFn func() error
	sendMsgFn   func(msg any) error
	recvMsgFn   func(msg any) error
}

func (m *mockClientStream) Header() (metadata.MD, error) {
	if m.headerFn != nil {
		return m.headerFn()
	}
	return metadata.MD{}, nil
}
func (m *mockClientStream) Trailer() metadata.MD          { return metadata.MD{} }
func (m *mockClientStream) CloseSend() error {
	if m.closeSendFn != nil {
		return m.closeSendFn()
	}
	return nil
}
func (m *mockClientStream) Context() context.Context { return context.Background() }
func (m *mockClientStream) SendMsg(msg any) error {
	if m.sendMsgFn != nil {
		return m.sendMsgFn(msg)
	}
	return nil
}
func (m *mockClientStream) RecvMsg(msg any) error {
	if m.recvMsgFn != nil {
		return m.recvMsgFn(msg)
	}
	return nil
}

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

			sentry.Flush(testutils.FlushTimeout())

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
			sentry.Flush(testutils.FlushTimeout())

			assert.Nil(t, clientStream, "ClientStream should be nil in this test scenario")
			// Pass the transport to the assertions to verify captured events.
			test.assertions(t, transport)
		})
	}
}

func TestSentryClientStream(t *testing.T) {
	newInterceptorWithStream := func(t *testing.T, inner grpc.ClientStream) (grpc.ClientStream, *sentry.MockTransport) {
		t.Helper()
		transport := &sentry.MockTransport{}
		sentry.Init(sentry.ClientOptions{
			Transport:        transport,
			EnableTracing:    true,
			TracesSampleRate: 1.0,
		})
		interceptor := sentrygrpc.StreamClientInterceptor()
		streamer := func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
			return inner, nil
		}
		stream, err := interceptor(context.Background(), &grpc.StreamDesc{}, nil, "/test.Service/Method", streamer)
		assert.NoError(t, err)
		return stream, transport
	}

	t.Run("RecvMsg EOF finishes span with OK", func(t *testing.T) {
		mock := &mockClientStream{
			recvMsgFn: func(msg any) error { return io.EOF },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		err := stream.RecvMsg(nil)
		assert.Equal(t, io.EOF, err)

		sentry.Flush(testutils.FlushTimeout())
		events := transport.Events()
		assert.Len(t, events, 1)
		assert.Equal(t, int(codes.OK), events[0].Contexts["trace"]["data"].(map[string]interface{})["rpc.grpc.status_code"])
	})

	t.Run("RecvMsg error finishes span with error status", func(t *testing.T) {
		rpcErr := status.Error(codes.Unavailable, "down")
		mock := &mockClientStream{
			recvMsgFn: func(msg any) error { return rpcErr },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		err := stream.RecvMsg(nil)
		assert.Equal(t, rpcErr, err)

		sentry.Flush(testutils.FlushTimeout())
		events := transport.Events()
		assert.Len(t, events, 1)
		assert.Equal(t, int(codes.Unavailable), events[0].Contexts["trace"]["data"].(map[string]interface{})["rpc.grpc.status_code"])
	})

	t.Run("CloseSend error finishes span", func(t *testing.T) {
		rpcErr := status.Error(codes.Internal, "internal")
		mock := &mockClientStream{
			closeSendFn: func() error { return rpcErr },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		err := stream.CloseSend()
		assert.Equal(t, rpcErr, err)

		sentry.Flush(testutils.FlushTimeout())
		events := transport.Events()
		assert.Len(t, events, 1)
		assert.Equal(t, int(codes.Internal), events[0].Contexts["trace"]["data"].(map[string]interface{})["rpc.grpc.status_code"])
	})

	t.Run("SendMsg error finishes span", func(t *testing.T) {
		rpcErr := status.Error(codes.DeadlineExceeded, "timeout")
		mock := &mockClientStream{
			sendMsgFn: func(msg any) error { return rpcErr },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		err := stream.SendMsg(nil)
		assert.Equal(t, rpcErr, err)

		sentry.Flush(testutils.FlushTimeout())
		events := transport.Events()
		assert.Len(t, events, 1)
		assert.Equal(t, int(codes.DeadlineExceeded), events[0].Contexts["trace"]["data"].(map[string]interface{})["rpc.grpc.status_code"])
	})

	t.Run("Header error finishes span", func(t *testing.T) {
		rpcErr := status.Error(codes.NotFound, "not found")
		mock := &mockClientStream{
			headerFn: func() (metadata.MD, error) { return nil, rpcErr },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		_, err := stream.Header()
		assert.Equal(t, rpcErr, err)

		sentry.Flush(testutils.FlushTimeout())
		events := transport.Events()
		assert.Len(t, events, 1)
		assert.Equal(t, int(codes.NotFound), events[0].Contexts["trace"]["data"].(map[string]interface{})["rpc.grpc.status_code"])
	})

	t.Run("finish is idempotent", func(t *testing.T) {
		rpcErr := status.Error(codes.Canceled, "canceled")
		mock := &mockClientStream{
			recvMsgFn: func(msg any) error { return rpcErr },
			closeSendFn: func() error { return rpcErr },
		}
		stream, transport := newInterceptorWithStream(t, mock)

		// Trigger finish via two different paths.
		stream.RecvMsg(nil)
		stream.CloseSend()

		sentry.Flush(testutils.FlushTimeout())
		// Only one transaction should be recorded.
		events := transport.Events()
		assert.Len(t, events, 1)
	})
}
