package sentrygrpc_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/getsentry/sentry-go/internal/sentrytest"
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
	headerFn  func() (metadata.MD, error)
	contextFn func() context.Context
	sendMsgFn func(msg any) error
	recvMsgFn func(msg any) error
}

func (m *mockClientStream) Header() (metadata.MD, error) {
	if m.headerFn != nil {
		return m.headerFn()
	}
	return metadata.MD{}, nil
}
func (m *mockClientStream) Trailer() metadata.MD { return metadata.MD{} }
func (m *mockClientStream) CloseSend() error     { return nil }
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
		DataCollection:   &sentry.DataCollection{},
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))
	return transport
}

func spanStatusCode(t *testing.T, transport *sentry.MockTransport) int {
	t.Helper()
	events := transport.Events()
	require.Len(t, events, 1)
	for _, span := range events[0].Spans {
		if span.Op == "rpc.client" {
			return span.Data["rpc.grpc.status_code"].(int)
		}
	}
	t.Fatal("missing rpc.client span")
	return 0
}

func startClientTransaction(base context.Context, t *testing.T) (context.Context, *sentry.Span) {
	t.Helper()
	ctx, _ := sentry.WithIsolationScope(base)
	transaction := sentry.StartTransaction(ctx, "test client transaction")
	return transaction.Context(), transaction
}

func requireUnaryClientBaggagePropagation(
	t *testing.T,
	existingBaggage string,
	assertBaggage func(t *testing.T, baggageHeader string),
) {
	t.Helper()

	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryClientInterceptor()
	ctx, transaction := startClientTransaction(context.Background(), t)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		sentry.SentryBaggageHeader, existingBaggage,
	))

	err := interceptor(ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
		md, ok := metadata.FromOutgoingContext(ctx)
		require.True(t, ok)
		assertBaggage(t, strings.Join(md.Get(sentry.SentryBaggageHeader), ","))
		return nil
	})

	require.NoError(t, err)
	transaction.Finish()
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestUnaryClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		ctx       context.Context
		invoker   grpc.UnaryInvoker
		assertErr assert.ErrorAssertionFunc
		wantCode  codes.Code
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
			assertErr: assert.NoError,
			wantCode:  codes.OK,
		},
		"records span with error status on handler error": {
			ctx: context.Background(),
			invoker: func(_ context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				return status.Error(codes.NotFound, "not found")
			},
			assertErr: assert.Error,
			wantCode:  codes.NotFound,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			transport := initMockTransport(t)
			interceptor := sentrygrpc.UnaryClientInterceptor()
			ctx, transaction := startClientTransaction(tc.ctx, t)

			err := interceptor(ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, tc.invoker)
			tc.assertErr(t, err)
			transaction.Finish()
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
	ctx, transaction := startClientTransaction(context.Background(), t)
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
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
	transaction.Finish()
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestUnaryClientInterceptor_PreservesExistingBaggageMembers(t *testing.T) {
	requireUnaryClientBaggagePropagation(t, "othervendor=bla", func(t *testing.T, baggageHeader string) {
		assert.Contains(t, baggageHeader, "othervendor=bla")
		assert.Contains(t, baggageHeader, "sentry-trace_id")
	})
}

func TestUnaryClientInterceptor_PropagatesSentryBaggageWhenExistingBaggageIsMalformed(t *testing.T) {
	requireUnaryClientBaggagePropagation(t, "not-valid", func(t *testing.T, baggageHeader string) {
		assert.NotContains(t, baggageHeader, "not-valid")
		assert.Contains(t, baggageHeader, "sentry-trace_id")
	})
}

func TestUnaryClientInterceptor_PropagatesScopeWithoutSpan(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		continued bool
		existing  []string
		want      string
	}{
		{name: "generated", existing: []string{"othervendor=value", "sentry-release=old,sentry-extra=stale"}},
		{name: "frozen empty", continued: true, existing: []string{"othervendor=value", "sentry-release=old,sentry-extra=stale"}, want: "othervendor=value"},
		{name: "frozen empty with malformed baggage", continued: true, existing: []string{"not-valid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{Release: "scope-release"}))
			ctx := f.NewContext(context.Background())
			if test.continued {
				sentry.StartTransaction(ctx, "incoming", sentry.ContinueTrace("11111111111111111111111111111111-2222222222222222-1", "")).Finish()
			}
			wantTrace := sentry.GetTraceparent(ctx)
			require.NotEmpty(t, wantTrace)
			original := metadata.MD{sentry.SentryBaggageHeader: test.existing}
			ctx = metadata.NewOutgoingContext(ctx, original)
			err := sentrygrpc.UnaryClientInterceptor()(ctx, "/test.TestService/Method", struct{}{}, struct{}{}, nil, func(ctx context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption) error {
				require.Nil(t, sentry.SpanFromContext(ctx))
				md, ok := metadata.FromOutgoingContext(ctx)
				require.True(t, ok)
				require.Equal(t, []string{wantTrace}, md.Get(sentry.SentryTraceHeader))
				value := strings.Join(md.Get(sentry.SentryBaggageHeader), ",")
				if test.continued {
					require.Equal(t, test.want, value)
				} else {
					require.Contains(t, value, "othervendor=value")
					require.Contains(t, value, "sentry-release=scope-release")
					require.NotContains(t, value, "sentry-release=old")
					require.NotContains(t, value, "sentry-extra=stale")
				}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, test.existing, original.Get(sentry.SentryBaggageHeader))
			f.Flush()
			require.Empty(t, f.Events())
		})
	}
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
			streamOp: func(stream grpc.ClientStream) { require.ErrorIs(t, stream.RecvMsg(nil), io.EOF) },
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
		"RecvMsg error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{recvMsgFn: func(_ any) error { return status.Error(codes.Unavailable, "down") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { require.Error(t, stream.RecvMsg(nil)) },
			wantCode: codes.Unavailable,
		},
		"SendMsg error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{sendMsgFn: func(_ any) error { return status.Error(codes.DeadlineExceeded, "timeout") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { require.Error(t, stream.SendMsg(nil)) },
			wantCode: codes.DeadlineExceeded,
		},
		"Header error records error status": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				return &mockClientStream{headerFn: func() (metadata.MD, error) { return nil, status.Error(codes.NotFound, "not found") }}, nil
			},
			streamOp: func(stream grpc.ClientStream) { _, err := stream.Header(); require.Error(t, err) },
			wantCode: codes.NotFound,
		},
		"finish is idempotent across multiple error paths": {
			ctx: context.Background(),
			streamer: func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
				rpcErr := status.Error(codes.Canceled, "canceled")
				return &mockClientStream{
					sendMsgFn: func(_ any) error { return rpcErr },
					recvMsgFn: func(_ any) error { return rpcErr },
				}, nil
			},
			streamOp: func(stream grpc.ClientStream) {
				require.Error(t, stream.SendMsg(nil))
				require.Error(t, stream.RecvMsg(nil))
			},
			wantCode: codes.Canceled,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			transport := initMockTransport(t)
			interceptor := sentrygrpc.StreamClientInterceptor()
			ctx, transaction := startClientTransaction(tc.ctx, t)

			stream, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.TestService/Method", tc.streamer)
			if tc.wantCode == codes.OK {
				require.NoError(t, err)
			}
			if tc.streamOp != nil && stream != nil {
				tc.streamOp(stream)
			}

			transaction.Finish()
			sentry.Flush(testutils.FlushTimeout())
			assert.Equal(t, int(tc.wantCode), spanStatusCode(t, transport))
		})
	}
}

func TestStreamClientInterceptor_FinishesNonServerStreamingResponseOnFirstRecv(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamClientInterceptor()
	ctx, transaction := startClientTransaction(context.Background(), t)

	stream, err := interceptor(ctx, &grpc.StreamDesc{}, nil, "/test.TestService/Method", func(_ context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		return &mockClientStream{recvMsgFn: func(_ any) error { return nil }}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NoError(t, stream.RecvMsg(nil))

	transaction.Finish()
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestStreamClientInterceptor_CloseSendWaitsForRecvMsg(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamClientInterceptor()
	ctx, transaction := startClientTransaction(context.Background(), t)
	var rpcSpan *sentry.Span

	stream, err := interceptor(ctx, &grpc.StreamDesc{ClientStreams: true}, nil, "/test.TestService/Method", func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		rpcSpan = sentry.SpanFromContext(ctx)
		return &mockClientStream{recvMsgFn: func(_ any) error { return nil }}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NotNil(t, rpcSpan)
	require.NoError(t, stream.CloseSend())
	assert.True(t, rpcSpan.EndTime.IsZero(), "CloseSend must leave the RPC span unfinished")

	require.NoError(t, stream.RecvMsg(nil))
	transaction.Finish()
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.OK), spanStatusCode(t, transport))
}

func TestStreamClientInterceptor_SendMsgEOFWaitsForRecvMsgStatus(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamClientInterceptor()
	ctx, transaction := startClientTransaction(context.Background(), t)
	var rpcSpan *sentry.Span

	stream, err := interceptor(ctx, &grpc.StreamDesc{ClientStreams: true, ServerStreams: true}, nil, "/test.TestService/Method", func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
		rpcSpan = sentry.SpanFromContext(ctx)
		return &mockClientStream{
			sendMsgFn: func(_ any) error { return io.EOF },
			recvMsgFn: func(_ any) error { return status.Error(codes.Unavailable, "down") },
		}, nil
	})

	require.NoError(t, err)
	require.NotNil(t, stream)
	require.NotNil(t, rpcSpan)
	assert.ErrorIs(t, stream.SendMsg(nil), io.EOF)
	assert.True(t, rpcSpan.EndTime.IsZero(), "SendMsg EOF must leave the RPC span unfinished")

	err = stream.RecvMsg(nil)
	require.Error(t, err)
	assert.Equal(t, codes.Unavailable, status.Code(err))

	transaction.Finish()
	sentry.Flush(testutils.FlushTimeout())
	assert.Equal(t, int(codes.Unavailable), spanStatusCode(t, transport))
}

func TestStreamClientInterceptor_FinishesOnContextCancellation(t *testing.T) {
	t.Parallel()

	sentrytest.Run(t, func(t *testing.T, fixture *sentrytest.Fixture) {
		ctx, transaction := startClientTransaction(fixture.NewContext(context.Background()), t)
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		var rpcSpan *sentry.Span
		stream, err := sentrygrpc.StreamClientInterceptor()(ctx, &grpc.StreamDesc{}, nil, "/test.TestService/Method", func(ctx context.Context, _ *grpc.StreamDesc, _ *grpc.ClientConn, _ string, _ ...grpc.CallOption) (grpc.ClientStream, error) {
			rpcSpan = sentry.SpanFromContext(ctx)
			return &mockClientStream{}, nil
		})
		require.NoError(t, err)
		require.NotNil(t, stream)
		require.NotNil(t, rpcSpan)
		require.True(t, rpcSpan.EndTime.IsZero())

		cancel()
		synctest.Wait()
		require.False(t, rpcSpan.EndTime.IsZero(), "cancellation must finish the RPC span without another stream operation")
		transaction.Finish()
		fixture.Flush()
		assert.Equal(t, int(codes.Canceled), spanStatusCode(t, fixture.Transport))
	}, sentrytest.WithClientOptions(sentry.ClientOptions{
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	}))
}
