package sentrygrpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const defaultClientOperationName = "grpc.client"

func TestClientOptions_SetDefaults(t *testing.T) {
	tests := map[string]struct {
		options    sentrygrpc.ClientOptions
		assertions func(t *testing.T, options sentrygrpc.ClientOptions)
	}{
		"Defaults are set when fields are empty": {
			options: sentrygrpc.ClientOptions{},
			assertions: func(t *testing.T, options sentrygrpc.ClientOptions) {
				assert.NotNil(t, options.ReportOn, "ReportOn should be set to default function")
				assert.Equal(t, defaultClientOperationName, options.OperationName, "OperationName should be set to default value")
			},
		},
		"Custom ReportOn is preserved": {
			options: sentrygrpc.ClientOptions{
				ReportOn: func(err error) bool {
					return err.Error() == "custom error"
				},
			},
			assertions: func(t *testing.T, options sentrygrpc.ClientOptions) {
				assert.NotNil(t, options.ReportOn, "ReportOn should not be nil")
				err := errors.New("random error")
				assert.False(t, options.ReportOn(err), "ReportOn should return false for random error")
			},
		},
		"Custom OperationName is preserved": {
			options: sentrygrpc.ClientOptions{
				OperationName: "custom.operation",
			},
			assertions: func(t *testing.T, options sentrygrpc.ClientOptions) {
				assert.Equal(t, "custom.operation", options.OperationName, "OperationName should be set to custom value")
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

func TestUnaryClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		invoker     grpc.UnaryInvoker
		options     sentrygrpc.ClientOptions
		expectedErr error
		assertions  func(t *testing.T, transport *sentry.TransportMock)
	}{
		"Default behavior, no error": {
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return nil
			},
			options: sentrygrpc.ClientOptions{},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				assert.Empty(t, transport.Events(), "No events should be captured")
			},
		},
		"Error is reported": {
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return errors.New("test error")
			},
			options:     sentrygrpc.ClientOptions{},
			expectedErr: errors.New("test error"),
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				events := transport.Events()
				assert.Len(t, events, 1, "One event should be captured")
				assert.Equal(t, "test error", events[0].Exception[0].Value, "Captured exception should match the error")
			},
		},
		"Metadata propagation": {
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				md, ok := metadata.FromOutgoingContext(ctx)
				assert.True(t, ok, "Metadata should be present in the outgoing context")
				assert.Contains(t, md, sentry.SentryTraceHeader, "Metadata should contain Sentry trace header")
				assert.Contains(t, md, sentry.SentryBaggageHeader, "Metadata should contain Sentry baggage header")
				return nil
			},
			options:    sentrygrpc.ClientOptions{},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {},
		},
		"Custom ReportOn behavior": {
			invoker: func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, opts ...grpc.CallOption) error {
				return errors.New("test error")
			},
			options: sentrygrpc.ClientOptions{
				ReportOn: func(err error) bool {
					return err.Error() == "specific error"
				},
			},
			expectedErr: errors.New("test error"),
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				assert.Empty(t, transport.Events(), "No events should be captured due to custom ReportOn")
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			transport := &sentry.TransportMock{}
			sentry.Init(sentry.ClientOptions{
				Transport: transport,
			})

			interceptor := sentrygrpc.UnaryClientInterceptor(test.options)

			// Execute the interceptor
			err := interceptor(context.Background(), "/test.Service/TestMethod", struct{}{}, struct{}{}, nil, test.invoker)

			if test.expectedErr != nil {
				assert.Equal(t, test.expectedErr, err, "Expected error mismatch")
			} else {
				assert.NoError(t, err, "Expected no error")
			}

			sentry.Flush(2 * time.Second)

			// Pass the transport to the assertions to verify captured events.
			test.assertions(t, transport)
		})
	}
}

func TestStreamClientInterceptor(t *testing.T) {
	tests := map[string]struct {
		streamer    grpc.Streamer
		options     sentrygrpc.ClientOptions
		expectedErr error
		assertions  func(t *testing.T, transport *sentry.TransportMock)
		streamDesc  *grpc.StreamDesc
	}{
		"Default behavior, no error": {
			streamer: func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, nil
			},
			options: sentrygrpc.ClientOptions{},
			streamDesc: &grpc.StreamDesc{
				ClientStreams: true,
			},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				assert.Empty(t, transport.Events(), "No events should be captured")
			},
		},
		"Error is reported": {
			streamer: func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, errors.New("test stream error")
			},
			options:     sentrygrpc.ClientOptions{},
			expectedErr: errors.New("test stream error"),
			streamDesc:  &grpc.StreamDesc{},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				events := transport.Events()
				assert.Len(t, events, 1, "One event should be captured")
				assert.Equal(t, "test stream error", events[0].Exception[0].Value, "Captured exception should match the error")
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
			options: sentrygrpc.ClientOptions{},
			streamDesc: &grpc.StreamDesc{
				ClientStreams: true,
			},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {},
		},
		"Custom ReportOn behavior": {
			streamer: func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
				return nil, errors.New("test stream error")
			},
			options: sentrygrpc.ClientOptions{
				ReportOn: func(err error) bool {
					return err.Error() == "specific error"
				},
			},
			expectedErr: errors.New("test stream error"),
			streamDesc:  &grpc.StreamDesc{},
			assertions: func(t *testing.T, transport *sentry.TransportMock) {
				assert.Empty(t, transport.Events(), "No events should be captured due to custom ReportOn")
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			// Reinitialize the transport for each test to ensure isolation.
			transport := &sentry.TransportMock{}
			sentry.Init(sentry.ClientOptions{
				Transport: transport,
			})

			interceptor := sentrygrpc.StreamClientInterceptor(test.options)

			// Execute the interceptor
			clientStream, err := interceptor(context.Background(), test.streamDesc, nil, "/test.Service/TestMethod", test.streamer)

			if test.expectedErr != nil {
				assert.Equal(t, test.expectedErr, err, "Expected error mismatch")
			} else {
				assert.NoError(t, err, "Expected no error")
			}

			sentry.Flush(2 * time.Second)

			assert.Nil(t, clientStream, "ClientStream should be nil in this test scenario")
			// Pass the transport to the assertions to verify captured events.
			test.assertions(t, transport)
		})
	}
}
