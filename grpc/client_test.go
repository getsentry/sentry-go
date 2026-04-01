package sentrygrpc_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// mockClientStream implements grpc.ClientStream for testing.
type mockClientStream struct {
	headerFn    func() (metadata.MD, error)
	closeSendFn func() error
	contextFn   func() context.Context
	sendMsgFn   func(msg any) error
	recvMsgFn   func(msg any) error
}

func (m *mockClientStream) Header() (metadata.MD, error) {
	if m.headerFn != nil {
		return m.headerFn()
	}
	return metadata.MD{}, nil
}
func (m *mockClientStream) Trailer() metadata.MD { return metadata.MD{} }
func (m *mockClientStream) CloseSend() error {
	if m.closeSendFn != nil {
		return m.closeSendFn()
	}
	return nil
}
func (m *mockClientStream) Context() context.Context {
	if m.contextFn != nil {
		return m.contextFn()
	}
	return context.Background()
}
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

func initMockTransport(t *testing.T) *sentry.MockTransport {
	t.Helper()
	transport := &sentry.MockTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{
		Transport:        transport,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))
	return transport
}

func spanStatusCode(t *testing.T, transport *sentry.MockTransport) int {
	t.Helper()
	events := transport.Events()
	require.Len(t, events, 1)
	return events[0].Contexts["trace"]["data"].(map[string]any)["rpc.grpc.status_code"].(int)
}

func TestUnaryClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		ctx      context.Context
		invoker  grpc.UnaryInvoker
		wantCode codes.Code
	}{
		"records span and propagates trace headers": {
			ctx: metadata.NewOutgoingContext(context.Background(), metadata.Pairs("existing", "value")),
			invoker: func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				md, ok := metadata.FromOutgoingContext(ctx)
				require.True(t, ok)
				assert.Contains(t, md, sentry.SentryTraceHeader)
				assert.Contains(t, md, sentry.SentryBaggageHeader)
				assert.Contains(t, md, "existing")
				return nil
			},
			wantCode: codes.OK,
		},
		"records span with error status on handler error": {
			ctx: context.Background(),
			invoker: func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				return status.Error(codes.NotFound, "not found")
			},
			wantCode: codes.NotFound,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			transport := initMockTransport(t)
			interceptor := sentrygrpc.UnaryClientInterceptor()

			interceptor(tc.ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, tc.invoker)
			sentry.Flush(testutils.FlushTimeout())

			assert.Equal(t, int(tc.wantCode), spanStatusCode(t, transport))
		})
	}
}

func TestUnaryClientInterceptor_ReplacesExistingTraceHeaders(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryClientInterceptor()

	oldTrace := "0123456789abcdef0123456789abcdef-0123456789abcdef-1"
	oldBaggage := "sentry-trace_id=0123456789abcdef0123456789abcdef"
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		sentry.SentryTraceHeader, oldTrace,
		sentry.SentryBaggageHeader, oldBaggage,
		"existing", "value",
	))

	err := interceptor(ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		assert.Equal(t, []string{"value"}, md.Get("existing"))
		assert.Len(t, md.Get(sentry.SentryTraceHeader), 1)
		assert.Len(t, md.Get(sentry.SentryBaggageHeader), 1)
		assert.NotEqual(t, oldTrace, md.Get(sentry.SentryTraceHeader)[0])
		assert.NotEqual(t, oldBaggage, md.Get(sentry.SentryBaggageHeader)[0])
		return nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestUnaryClientInterceptor_PreservesExistingBaggageMembers(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryClientInterceptor()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs(
		sentry.SentryBaggageHeader, "othervendor=bla",
	))

	err := interceptor(ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		baggageHeader := strings.Join(md.Get(sentry.SentryBaggageHeader), ",")
		assert.Contains(t, baggageHeader, "othervendor=bla")
		assert.Contains(t, baggageHeader, "sentry-trace_id")
		return nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestStreamClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		ctx      context.Context
		streamer grpc.Streamer
		streamOp func(stream grpc.ClientStream)
		wantCode codes.Code
	}{
		"records span and propagates trace headers": {
			ctx: context.Background(),
			streamer: func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				md, ok := metadata.FromOutgoingContext(ctx)
				require.True(t, ok)
				assert.Contains(t, md, sentry.SentryTraceHeader)
				assert.Contains(t, md, sentry.SentryBaggageHeader)
				return &mockClientStream{recvMsgFn: func(_ any) error { return io.EOF }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.RecvMsg(nil) },
			wantCode: codes.OK,
		},
		"streamer error records span with error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, status.Error(codes.Unavailable, "unavailable")
			},
			wantCode: codes.Unavailable,
		},
		"nil stream from streamer records Internal error": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, nil
			},
			wantCode: codes.Internal,
		},
		"RecvMsg EOF finishes span with OK": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{recvMsgFn: func(_ any) error { return io.EOF }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.RecvMsg(nil) },
			wantCode: codes.OK,
		},
		"RecvMsg error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{recvMsgFn: func(_ any) error { return status.Error(codes.Unavailable, "down") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.RecvMsg(nil) },
			wantCode: codes.Unavailable,
		},
		"CloseSend error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{closeSendFn: func() error { return status.Error(codes.Internal, "internal") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.CloseSend() },
			wantCode: codes.Internal,
		},
		"SendMsg error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{sendMsgFn: func(_ any) error { return status.Error(codes.DeadlineExceeded, "timeout") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.SendMsg(nil) },
			wantCode: codes.DeadlineExceeded,
		},
		"Header error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{headerFn: func() (metadata.MD, error) { return nil, status.Error(codes.NotFound, "not found") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { stream.Header() },
			wantCode: codes.NotFound,
		},
		"finish is idempotent across multiple error paths": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				rpcErr := status.Error(codes.Canceled, "canceled")
				return &mockClientStream{
					recvMsgFn:   func(_ any) error { return rpcErr },
					closeSendFn: func() error { return rpcErr },
				}, nil
			},
			streamOp: func(stream grpc.ClientStream) {
				stream.RecvMsg(nil)
				stream.CloseSend()
			},
			wantCode: codes.Canceled,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			transport := initMockTransport(t)
			interceptor := sentrygrpc.StreamClientInterceptor()

			stream, _ := interceptor(tc.ctx, &grpc.StreamDesc{}, nil, "/test.TestService/Method", tc.streamer)
			if tc.streamOp != nil && stream != nil {
				tc.streamOp(stream)
			}

			sentry.Flush(testutils.FlushTimeout())
			assert.Equal(t, int(tc.wantCode), spanStatusCode(t, transport))
		})
	}
}

func TestStreamClientInterceptor_FinishesOnContextCancellation(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamClientInterceptor()

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.TestService/Method", func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		md, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		assert.Contains(t, md, sentry.SentryTraceHeader)
		assert.Contains(t, md, sentry.SentryBaggageHeader)
		return &mockClientStream{}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, stream)

	cancel()

	require.Eventually(t, func() bool {
		sentry.Flush(testutils.FlushTimeout())
		return len(transport.Events()) > 0
	}, testutils.FlushTimeout(), 10*time.Millisecond)

	events := transport.Events()
	lastEvent := events[len(events)-1]
	statusCode := lastEvent.Contexts["trace"]["data"].(map[string]any)["rpc.grpc.status_code"].(int)
	assert.Equal(t, int(codes.Canceled), statusCode)
}
