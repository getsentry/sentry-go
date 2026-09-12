package sentrygrpc_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/getsentry/sentry-go/internal/sentrytest"
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

type flushCountingTransport struct {
	sentry.MockTransport
	flushes atomic.Int32
}

func (t *flushCountingTransport) FlushWithContext(context.Context) bool {
	t.flushes.Add(1)
	return true
}

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
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "req-123"))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(ctx context.Context, _ any) (any, error) {
		require.NotNil(t, sentry.ScopeFromContext(ctx))
		require.NotNil(t, sentry.SpanFromContext(ctx))
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
			"metadata": map[string]any{"x-request-id": "req-123"},
		},
	}, summarizeTx(events[0])); diff != "" {
		t.Errorf("transaction mismatch (-want +got):\n%s", diff)
	}
}

func TestUnaryServerInterceptor_ContinuesIncomingTrace(t *testing.T) {
	const traceID = "0123456789abcdef0123456789abcdef"
	const parentSpanID = "0123456789abcdef"

	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		sentry.SentryTraceHeader, traceID+"-"+parentSpanID+"-1",
		sentry.SentryBaggageHeader, "sentry-release=1.0",
	))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(ctx context.Context, _ any) (any, error) {
		span := sentry.SpanFromContext(ctx)
		require.NotNil(t, span)
		assert.Equal(t, traceID, span.TraceID.String())
		assert.Equal(t, parentSpanID, span.ParentSpanID.String())
		return struct{}{}, nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())
	events := transport.Events()
	require.Len(t, events, 1)
	assert.Equal(t, traceID, events[0].Contexts["trace"]["trace_id"].(sentry.TraceID).String())
}

func TestUnaryServerInterceptor_RequestIsolation(t *testing.T) {
	const requestCount = 32
	f := sentrytest.NewFixture(t)
	parentCtx := f.NewContext(context.Background())
	parentScope := sentry.ScopeFromContext(parentCtx)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	scopes := make(chan *sentry.Scope, requestCount)
	errCh := make(chan error, requestCount)

	var wg sync.WaitGroup
	for range requestCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := interceptor(parentCtx, nil, &grpc.UnaryServerInfo{
				FullMethod: "/test.TestService/Method",
			}, func(ctx context.Context, _ any) (any, error) {
				scope := sentry.ScopeFromContext(ctx)
				if scope == nil {
					return nil, status.Error(codes.Internal, "request scope is nil")
				}
				if scope == parentScope {
					return nil, status.Error(codes.Internal, "request reused the parent scope")
				}
				if sentry.SpanFromContext(ctx) == nil {
					return nil, status.Error(codes.Internal, "request span is nil")
				}
				scopes <- scope
				return struct{}{}, nil
			})
			if err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	close(scopes)
	for err := range errCh {
		t.Error(err)
	}

	seen := make(map[*sentry.Scope]struct{}, requestCount)
	for scope := range scopes {
		if _, exists := seen[scope]; exists {
			t.Error("concurrent request reused an isolation scope")
		}
		seen[scope] = struct{}{}
	}
	if len(seen) != requestCount {
		t.Errorf("isolated scope count = %d, want %d", len(seen), requestCount)
	}
}

func TestUnaryServerInterceptor_ScrubsSensitiveMetadata(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(
		"authorization", "Bearer secret-token",
		"x-api-key", "top-secret",
		"cookie", "session=secret",
		"x-request-id", "req-123",
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
	if diff := cmp.Diff(
		map[string]any{
			"authorization": "[Filtered]",
			"cookie":        "session=[Filtered]",
			"x-api-key":     "[Filtered]",
			"x-request-id":  "req-123",
		}, metadataContext,
		testutils.EquateKeyValueStrings(),
	); diff != "" {
		t.Fatalf("span data mismatch (-want +got):\n%s", diff)
	}
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
			ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "req-123"))

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

func TestUnaryServerInterceptor_WaitsForPanicDelivery(t *testing.T) {
	transport := &flushCountingTransport{}
	require.NoError(t, sentry.Init(sentry.ClientOptions{Transport: transport}))
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{WaitForDelivery: true})

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(context.Context, any) (any, error) {
		panic("test panic")
	})

	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Positive(t, transport.flushes.Load())
}

func TestServerInterceptors_MapStatus(t *testing.T) {
	tests := map[string]struct {
		code       codes.Code
		wantStatus sentry.SpanStatus
		invoke     func(error) error
	}{
		"unary": {
			code:       codes.NotFound,
			wantStatus: sentry.SpanStatusNotFound,
			invoke: func(rpcErr error) error {
				interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
				_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
					FullMethod: "/test.TestService/Method",
				}, func(context.Context, any) (any, error) {
					return nil, rpcErr
				})
				return err
			},
		},
		"stream": {
			code:       codes.Unavailable,
			wantStatus: sentry.SpanStatusUnavailable,
			invoke: func(rpcErr error) error {
				interceptor := sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{})
				return interceptor(nil, &stubServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{
					FullMethod: "/test.TestService/StreamMethod",
				}, func(any, grpc.ServerStream) error {
					return rpcErr
				})
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			transport := initMockTransport(t)
			rpcErr := status.Error(tc.code, "test error")
			assert.ErrorIs(t, tc.invoke(rpcErr), rpcErr)
			sentry.Flush(testutils.FlushTimeout())

			events := transport.Events()
			require.Len(t, events, 1)
			summary := summarizeTx(events[0])
			assert.Equal(t, tc.wantStatus, summary.Status)
			assert.Equal(t, int(tc.code), summary.Data["rpc.grpc.status_code"])
		})
	}
}

func TestStreamServerInterceptor(t *testing.T) {
	transport := initMockTransport(t)
	interceptor := sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{})
	ss := &stubServerStream{
		ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "req-123")),
	}

	err := interceptor(nil, ss, &grpc.StreamServerInfo{
		FullMethod: "/test.TestService/StreamMethod",
	}, func(_ any, stream grpc.ServerStream) error {
		require.NotNil(t, sentry.ScopeFromContext(stream.Context()))
		require.NotNil(t, sentry.SpanFromContext(stream.Context()))
		md, ok := metadata.FromIncomingContext(stream.Context())
		require.True(t, ok)
		require.Contains(t, md, "x-request-id")
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
			"metadata": map[string]any{"x-request-id": "req-123"},
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
				ctx: metadata.NewIncomingContext(context.Background(), metadata.Pairs("x-request-id", "req-123")),
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
