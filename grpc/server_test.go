package sentrygrpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestServerOptions_SetDefaults(t *testing.T) {
	tests := map[string]struct {
		options    sentrygrpc.ServerOptions
		assertions func(t *testing.T, options sentrygrpc.ServerOptions)
	}{
		"Defaults are set when fields are empty": {
			options: sentrygrpc.ServerOptions{},
			assertions: func(t *testing.T, options sentrygrpc.ServerOptions) {
				assert.NotNil(t, options.ReportOn, "ReportOn should be set to default function")
				assert.Equal(t, sentry.DefaultFlushTimeout, options.Timeout, "Timeout should be set to default value")
			},
		},
		"Custom ReportOn is preserved": {
			options: sentrygrpc.ServerOptions{
				ReportOn: func(err error) bool {
					return err.Error() == "specific error"
				},
			},
			assertions: func(t *testing.T, options sentrygrpc.ServerOptions) {
				assert.NotNil(t, options.ReportOn, "ReportOn should not be nil")
				err := errors.New("random error")
				assert.False(t, options.ReportOn(err), "ReportOn should return false for random error")
				err = errors.New("specific error")
				assert.True(t, options.ReportOn(err), "ReportOn should return true for specific error")
			},
		},
		"Custom Timeout is preserved": {
			options: sentrygrpc.ServerOptions{
				Timeout: 5 * time.Second,
			},
			assertions: func(t *testing.T, options sentrygrpc.ServerOptions) {
				assert.Equal(t, 5*time.Second, options.Timeout, "Timeout should be set to custom value")
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			test.options.SetDefaults()

			test.assertions(t, test.options)
		})
	}
}

func TestUnaryServerInterceptor(t *testing.T) {
	tests := map[string]struct {
		options           sentrygrpc.ServerOptions
		handler           grpc.UnaryHandler
		expectedErr       string
		wantException     string
		wantTransaction   *sentry.Event
		assertTransaction bool
	}{
		"Handle panic and re-panic": {
			options: sentrygrpc.ServerOptions{Repanic: true},
			handler: func(ctx context.Context, req any) (any, error) {
				panic("test panic")
			},
		},
		"Report error with transaction": {
			options: sentrygrpc.ServerOptions{
				ReportOn: func(err error) bool {
					return true
				},
			},
			handler: func(ctx context.Context, req any) (any, error) {
				return nil, status.Error(codes.Internal, "handler error")
			},
			expectedErr:       "rpc error: code = Internal desc = handler error",
			wantException:     "rpc error: code = Internal desc = handler error",
			assertTransaction: true,
		},
		"Do not report error when ReportOn returns false": {
			options: sentrygrpc.ServerOptions{
				ReportOn: func(err error) bool {
					return false
				},
			},
			handler: func(ctx context.Context, req any) (any, error) {
				return nil, status.Error(codes.Internal, "handler error not reported")
			},
			expectedErr:       "rpc error: code = Internal desc = handler error not reported",
			assertTransaction: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			eventsCh := make(chan *sentry.Event, 1)
			transactionsCh := make(chan *sentry.Event, 1)

			err := sentry.Init(sentry.ClientOptions{
				BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
					eventsCh <- event
					return event
				},
				BeforeSendTransaction: func(tx *sentry.Event, hint *sentry.EventHint) *sentry.Event {
					fmt.Println("Transaction: ", tx.Transaction)
					transactionsCh <- tx
					return tx
				},
				EnableTracing:    true,
				TracesSampleRate: 1.0,
			})
			if err != nil {
				t.Fatal(err)
			}

			interceptor := sentrygrpc.UnaryServerInterceptor(test.options)

			defer func() {
				if r := recover(); r != nil {
					// Assert the panic message for tests with repanic enabled
					if test.options.Repanic {
						assert.Equal(t, "test panic", r, "Expected panic to propagate with message 'test panic'")
					}
				}
			}()

			_, err = interceptor(context.Background(), nil, &grpc.UnaryServerInfo{
				FullMethod: "TestService.Method",
			}, test.handler)

			if test.expectedErr != "" {
				assert.EqualError(t, err, test.expectedErr)
			} else {
				assert.NoError(t, err)
			}

			if test.wantException != "" {
				close(eventsCh)
				var gotEvent *sentry.Event
				for e := range eventsCh {
					gotEvent = e
				}

				assert.NotNil(t, gotEvent, "Expected an event")
				assert.Len(t, gotEvent.Exception, 1, "Expected one exception in the event")
				assert.Equal(t, test.wantException, gotEvent.Exception[0].Value, "Exception values should match")
			}

			if test.assertTransaction {
				close(transactionsCh)
				var gotTransaction *sentry.Event
				for tx := range transactionsCh {
					fmt.Println("Transaction: ", tx.Transaction)
					gotTransaction = tx
				}
				assert.NotNil(t, gotTransaction, "Expected a transaction")
				assert.Equal(t, fmt.Sprintf("UnaryServerInterceptor %s", "TestService.Method"), gotTransaction.Transaction, "Transaction names should match")
			}

			sentry.Flush(2 * time.Second)
		})
	}
}

// wrappedServerStream is a wrapper around grpc.ServerStream that overrides the Context method.
type wrappedServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the custom context for the stream.
func (w *wrappedServerStream) Context() context.Context {
	return w.ctx
}

func TestStreamServerInterceptor(t *testing.T) {
	tests := map[string]struct {
		options          sentrygrpc.ServerOptions
		handler          grpc.StreamHandler
		expectedErr      string
		expectedMetadata bool
		expectedEvent    bool
	}{
		"Default behavior, no error": {
			options: sentrygrpc.ServerOptions{},
			handler: func(srv any, stream grpc.ServerStream) error {
				return nil
			},
			expectedErr:      "",
			expectedMetadata: false,
			expectedEvent:    false,
		},
		"Handler returns an error": {
			options: sentrygrpc.ServerOptions{
				ReportOn: func(err error) bool {
					return true
				},
			},
			handler: func(srv any, stream grpc.ServerStream) error {
				return status.Error(codes.Internal, "stream error")
			},
			expectedErr:      "rpc error: code = Internal desc = stream error",
			expectedMetadata: false,
			expectedEvent:    true,
		},
		"Repanic is enabled": {
			options: sentrygrpc.ServerOptions{
				Repanic: true,
			},
			handler: func(srv any, stream grpc.ServerStream) error {
				panic("test panic")
			},
			expectedErr:      "",
			expectedMetadata: false,
			expectedEvent:    true,
		},
		"Metadata is propagated": {
			options: sentrygrpc.ServerOptions{},
			handler: func(srv any, stream grpc.ServerStream) error {
				md, ok := metadata.FromIncomingContext(stream.Context())
				if !ok || len(md) == 0 {
					return status.Error(codes.InvalidArgument, "metadata missing")
				}
				return nil
			},
			expectedErr:      "",
			expectedMetadata: true,
			expectedEvent:    false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {

			eventsCh := make(chan *sentry.Event, 1)
			transactionsCh := make(chan *sentry.Event, 1)

			err := sentry.Init(sentry.ClientOptions{
				BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
					eventsCh <- event
					return event
				},
				BeforeSendTransaction: func(tx *sentry.Event, hint *sentry.EventHint) *sentry.Event {
					transactionsCh <- tx
					return tx
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			interceptor := sentrygrpc.StreamServerInterceptor(test.options)

			// Simulate a server stream
			stream := &wrappedServerStream{
				ServerStream: nil,
				ctx:          metadata.NewIncomingContext(context.Background(), metadata.Pairs("key", "value")),
			}

			var recovered interface{}
			func() {
				defer func() {
					recovered = recover()
				}()
				err = interceptor(nil, stream, &grpc.StreamServerInfo{FullMethod: "TestService.StreamMethod"}, test.handler)
			}()

			if test.expectedErr != "" {
				assert.EqualError(t, err, test.expectedErr)
			} else {
				assert.NoError(t, err)
			}

			if test.expectedMetadata {
				md, ok := metadata.FromIncomingContext(stream.Context())
				assert.True(t, ok, "Expected metadata to be propagated in context")
				assert.Contains(t, md, "key", "Expected metadata to include 'key'")
			}

			if test.expectedEvent {
				close(eventsCh)
				var gotEvent *sentry.Event
				for e := range eventsCh {
					gotEvent = e
				}
				assert.NotNil(t, gotEvent, "Expected an event to be captured")
			} else {
				assert.Empty(t, eventsCh, "Expected no event to be captured")
			}

			if test.options.Repanic {
				assert.NotNil(t, recovered, "Expected panic to be re-raised")
				assert.Equal(t, "test panic", recovered, "Panic value should match")
			}

			sentry.Flush(2 * time.Second)
		})
	}
}
