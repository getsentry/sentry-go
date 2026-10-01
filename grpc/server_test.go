package sentrygrpc_test

import (
	"context"
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
		sentry.SentryBaggageHeader, "sentry-trace_id="+traceID,
	))

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{
		FullMethod: "/test.TestService/Method",
	}, func(ctx context.Context, _ any) (any, error) {
		span := sentry.SpanFromContext(ctx)
		require.NotNil(t, span)
		assert.Equal(t, traceID, span.TraceID.String())
		assert.Equal(t, parentSpanID, span.ParentSpanID.String())
		assert.Contains(t, span.ToBaggage(), "sentry-release=1.0")
		assert.Contains(t, span.ToBaggage(), "sentry-trace_id="+traceID)
		return struct{}{}, nil
	})

	require.NoError(t, err)
	sentry.Flush(testutils.FlushTimeout())
	events := transport.Events()
	require.Len(t, events, 1)
	assert.Equal(t, traceID, events[0].Contexts["trace"]["trace_id"].(sentry.TraceID).String())
	t.Run("reuses an active child without finishing its root", func(t *testing.T) {
		f := sentrytest.NewFixture(t, sentrytest.WithClientOptions(sentry.ClientOptions{EnableTracing: true, TracesSampleRate: 1}))
		outer := sentry.StartTransaction(f.NewContext(context.Background()), "gateway")
		child := sentry.StartSpan(outer.Context(), "gateway.grpc")
		type key struct{}
		ctx := context.WithValue(child.Context(), key{}, "preserved")
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.TestService/Method"}, func(ctx context.Context, _ any) (any, error) {
			require.Same(t, child, sentry.SpanFromContext(ctx))
			require.Same(t, outer, sentry.TransactionFromContext(ctx))
			require.Equal(t, "preserved", ctx.Value(key{}))
			return struct{}{}, nil
		})
		require.NoError(t, err)
		require.Equal(t, "gateway", outer.Name)
		require.True(t, outer.EndTime.IsZero(), "interceptor finished a borrowed transaction")
		child.Finish()
		outer.Finish()
		f.Flush()
		require.Len(t, f.Events(), 1)
		require.Equal(t, "transaction", f.Events()[0].Type)
	})
}

func TestUnaryServerInterceptor_RequestIsolation(t *testing.T) {
	t.Parallel()
	f := sentrytest.NewFixture(t)
	parentCtx := f.NewContext(context.Background())
	parentScope := sentry.ScopeFromContext(parentCtx)
	interceptor := sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})
	sentrytest.CheckRequestIsolation(t, func() (context.Context, context.Context, error) {
		ctx, cancel := context.WithCancel(parentCtx)
		defer cancel()
		var requestCtx context.Context
		_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/test.TestService/Method"}, func(ctx context.Context, _ any) (any, error) {
			requestCtx = ctx
			if sentry.ScopeFromContext(ctx) == parentScope {
				return nil, status.Error(codes.Internal, "request reused the parent scope")
			}
			return struct{}{}, nil
		})
		return requestCtx, nil, err
	})
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
		wantFlush   bool
	}{
		"panic is recovered and returns Internal error": {
			options: sentrygrpc.ServerOptions{},
		},
		"panic is re-panicked when Repanic is set": {
			options:     sentrygrpc.ServerOptions{Repanic: true},
			wantRepanic: true,
		},
		"panic waits for delivery": {
			options:   sentrygrpc.ServerOptions{WaitForDelivery: true},
			wantFlush: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			eventsCh := make(chan *sentry.Event, 1)
			transport := &flushCountingTransport{}
			require.NoError(t, sentry.Init(sentry.ClientOptions{
				Transport: transport,
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

			if tc.wantFlush {
				assert.Positive(t, transport.flushes.Load())
			}
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

func TestServerInterceptors_MapStatus(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       codes.Code
		wantStatus sentry.SpanStatus
	}{
		{"unary", codes.NotFound, sentry.SpanStatusNotFound},
		{"stream", codes.Unavailable, sentry.SpanStatusUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := initMockTransport(t)
			rpcErr := status.Error(tc.code, "test error")
			var err error
			if tc.name == "unary" {
				_, err = sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{})(context.Background(), nil,
					&grpc.UnaryServerInfo{FullMethod: "/test.TestService/Method"},
					func(context.Context, any) (any, error) { return nil, rpcErr })
			} else {
				err = sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{})(nil,
					&stubServerStream{ctx: context.Background()},
					&grpc.StreamServerInfo{FullMethod: "/test.TestService/StreamMethod"},
					func(any, grpc.ServerStream) error { return rpcErr })
			}
			assert.ErrorIs(t, err, rpcErr)
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
