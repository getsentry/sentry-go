package sentrygrpc_test

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// stubServerStream provides a minimal grpc.ServerStream for testing.
type stubServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *stubServerStream) Context() context.Context { return s.ctx }

// txSummary is a comparable snapshot of the span/transaction fields we assert.
type txSummary struct {
	Name   string
	Op     string
	Status sentry.SpanStatus
	Data   map[string]any
	GRPC   map[string]any
}

func summarizeTx(tx *sentry.Event) txSummary {
	s := txSummary{
		Name:   tx.Transaction,
		Op:     tx.Contexts["trace"]["op"].(string),
		Status: tx.Contexts["trace"]["status"].(sentry.SpanStatus),
		Data:   tx.Contexts["trace"]["data"].(map[string]any),
	}
	if g, ok := tx.Contexts["grpc"]; ok {
		s.GRPC = g
	}
	return s
}

func TestUnaryServerInterceptor(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("key", "value"))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(_ context.Context, _ any) (any, error) {
		return struct{}{}, nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())

	events := transport.Events()
	require.Len(t, events, 1)
	if diff := cmp.Diff(txSummary{
		Name:   "test.TestService/Method",
		Op:     "rpc.server",
		Status: sentry.SpanStatusOK,
		Data: map[string]any{
			"rpc.system":           "grpc",
			"rpc.service":          "test.TestService",
			"rpc.method":           "Method",
			"rpc.grpc.status_code": int(codes.OK),
		},
		GRPC: map[string]any{
			"method":   "test.TestService/Method",
			"metadata": map[string]any{"key": "value"},
		},
	}, summarizeTx(events[0])); diff != "" {
		t.Errorf("transaction mismatch (-want +got):\n%s", diff)
	}
}

func TestUnaryServerInterceptor_ScrubsSensitiveMetadata(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer secret-token",
		"x-api-key", "top-secret",
		"cookie", "session=secret",
		"key", "value",
	))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(_ context.Context, _ any) (any, error) {
		return struct{}{}, nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())

	events := transport.Events()
	require.Len(t, events, 1)
	grpcContext := events[0].Contexts["grpc"]
	metadataContext, ok := grpcContext["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"key": "value"}, metadataContext)
}

func TestUnaryServerInterceptor_Panic(t *testing.T) {
	tests := map[string]struct {
		options     sentrygrpc.ServerOptions
		wantRepanic bool
	}{
		"panic is recovered and returns Internal error": {
			options: sentrygrpc.ServerOptions{},
		},
		"panic is re-panicked when Repanic is set": {
			options:     sentrygrpc.ServerOptions{Repanic: true},
			wantRepanic: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			eventsCh := make(chan *sentry.Event, 1)
			require.NoError(t, sentry.Init(sentry.ClientOptions{
				BeforeSend: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					eventsCh <- e
					return e
				},
				EnableTracing:    true,
				TracesSampleRate: 1.0,
			}))

			interceptor := sentrygrpc.UnaryServerInterceptor(tc.options)
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("key", "value"))

			var (
				err       error
				recovered any
			)
			func() {
				defer func() { recovered = recover() }()
				_, err = interceptor(ctx, nil, &grpc.UnaryServerInfo{
					FullMethod: "/test.TestService/Method",
				}, func(context.Context, any) (any, error) {
					panic("test panic")
				})
			}()

			sentry.Flush(testutils.FlushTimeout())
			require.NotNil(t, <-eventsCh)

			if tc.wantRepanic {
				assert.Equal(t, "test panic", recovered)
			} else {
				assert.Nil(t, recovered)
				assert.Equal(t, codes.Internal, status.Code(err))
			}
		})
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{})
	ss := &stubServerStream{
		ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("key", "value")),
	}

	err := interceptor(nil, ss, &grpc.StreamServerInfo{
		FullMethod: "/test.TestService/StreamMethod",
	}, func(_ any, stream grpc.ServerStream) error {
		md, ok := metadata.FromIncomingContext(stream.Context())
		require.True(t, ok)
		require.Contains(t, md, "key")
		return nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())

	events := transport.Events()
	require.Len(t, events, 1)
	if diff := cmp.Diff(txSummary{
		Name:   "test.TestService/StreamMethod",
		Op:     "rpc.server",
		Status: sentry.SpanStatusOK,
		Data: map[string]any{
			"rpc.system":           "grpc",
			"rpc.service":          "test.TestService",
			"rpc.method":           "StreamMethod",
			"rpc.grpc.status_code": int(codes.OK),
		},
		GRPC: map[string]any{
			"method":   "test.TestService/StreamMethod",
			"metadata": map[string]any{"key": "value"},
		},
	}, summarizeTx(events[0])); diff != "" {
		t.Errorf("transaction mismatch (-want +got):\n%s", diff)
	}
}

func TestStreamServerInterceptor_Panic(t *testing.T) {
	tests := map[string]struct {
		options     sentrygrpc.ServerOptions
		wantRepanic bool
	}{
		"panic is recovered and returns Internal error": {
			options: sentrygrpc.ServerOptions{},
		},
		"panic is re-panicked when Repanic is set": {
			options:     sentrygrpc.ServerOptions{Repanic: true},
			wantRepanic: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			eventsCh := make(chan *sentry.Event, 1)
			require.NoError(t, sentry.Init(sentry.ClientOptions{
				BeforeSend: func(e *sentry.Event, _ *sentry.EventHint) *sentry.Event {
					eventsCh <- e
					return e
				},
				EnableTracing:    true,
				TracesSampleRate: 1.0,
			}))

			interceptor := sentrygrpc.StreamServerInterceptor(tc.options)
			ss := &stubServerStream{
				ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("key", "value")),
			}

			var (
				err       error
				recovered any
			)
			func() {
				defer func() { recovered = recover() }()
				err = interceptor(nil, ss, &grpc.StreamServerInfo{
					FullMethod: "/test.TestService/StreamMethod",
				}, func(_ any, _ grpc.ServerStream) error {
					panic("test panic")
				})
			}()

			sentry.Flush(testutils.FlushTimeout())
			require.NotNil(t, <-eventsCh)

			if tc.wantRepanic {
				assert.Equal(t, "test panic", recovered)
			} else {
				assert.Nil(t, recovered)
				assert.Equal(t, codes.Internal, status.Code(err))
			}
		})
	}
}
