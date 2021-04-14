package sentrygrpc_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestUnaryServerInterceptor(t *testing.T) {
	for _, tt := range []struct {
		name    string
		ctx     context.Context
		opts    []sentrygrpc.Option
		handler func(
			context.Context,
			*grpchealth.HealthCheckRequest,
		) (*grpchealth.HealthCheckResponse, error)
		code   codes.Code
		events []*sentry.Event
	}{
		{
			name: "does not report when err is nil",
			ctx:  context.Background(),
			handler: func(
				ctx context.Context,
				_ *grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				return &grpchealth.HealthCheckResponse{}, nil
			},
			code: codes.OK,
		},
		{
			name: "reports all errors by default",
			ctx:  context.Background(),
			handler: func(
				context.Context,
				*grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				return nil, status.Error(codes.NotFound, "not found")
			},
			code: codes.NotFound,
			events: []*sentry.Event{
				{
					Level: sentry.LevelError,
					Exception: []sentry.Exception{
						{
							Type:  "*status.Error",
							Value: "rpc error: code = NotFound desc = not found",
						},
					},
				},
			},
		},
		{
			name: "reports errors that ReportOn returns true",
			ctx:  context.Background(),
			opts: []sentrygrpc.Option{
				sentrygrpc.WithReportOn(
					func(err error) bool {
						return errors.Is(err, grpc.ErrServerStopped)
					},
				),
			},
			handler: func(
				context.Context,
				*grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				return nil, grpc.ErrServerStopped
			},
			code: codes.Unknown,
			events: []*sentry.Event{
				{
					Level: sentry.LevelError,
					Exception: []sentry.Exception{
						{
							Type:  "*errors.errorString",
							Value: "grpc: the server has been stopped",
						},
					},
				},
			},
		},
		{
			name: "does not report errors that ReportOn returns false",
			ctx:  context.Background(),
			opts: []sentrygrpc.Option{
				sentrygrpc.WithReportOn(
					func(err error) bool {
						return false
					},
				),
			},
			handler: func(
				context.Context,
				*grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				return nil, grpc.ErrServerStopped
			},
			code: codes.Unknown,
		},
		{
			name: "recovers from panic and returns internal error",
			ctx:  context.Background(),
			handler: func(
				context.Context,
				*grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				panic("simulated panic")
			},
			code: codes.Internal,
			events: []*sentry.Event{
				{
					Level:   sentry.LevelFatal,
					Message: "simulated panic",
				},
			},
		},
		{
			name: "sets hub on context",
			ctx:  context.Background(),
			handler: func(
				ctx context.Context,
				_ *grpchealth.HealthCheckRequest,
			) (*grpchealth.HealthCheckResponse, error) {
				if !sentry.HasHubOnContext(ctx) {
					t.Fatal("context must have hub")
				}

				return &grpchealth.HealthCheckResponse{}, nil
			},
			code: codes.OK,
		},
	} {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			var events []*sentry.Event

			if err := sentry.Init(sentry.ClientOptions{
				BeforeSend: func(
					event *sentry.Event,
					hint *sentry.EventHint,
				) *sentry.Event {
					events = append(events, event)
					return event
				},
			}); err != nil {
				t.Fatalf("sentry.Init error: %s", err)
			}

			lis, err := net.Listen("tcp", "localhost:0")
			if err != nil {
				t.Fatalf("net.Listen error: %s", err)
			}
			defer lis.Close()

			opt := grpc.UnaryInterceptor(sentrygrpc.UnaryServerInterceptor(tt.opts...))
			server := grpc.NewServer(opt)
			defer server.Stop()

			grpchealth.RegisterHealthServer(
				server,
				&mockHealthServer{handler: tt.handler},
			)

			go func() {
				if err := server.Serve(lis); err != nil {
					t.Errorf("grpc serve error: %s", err)
				}
			}()

			conn, err := grpc.Dial(lis.Addr().String(), grpc.WithInsecure())
			if err != nil {
				t.Fatalf("grpc.Dial error: %s", err)
			}
			defer conn.Close()

			client := grpchealth.NewHealthClient(conn)

			req := &grpchealth.HealthCheckRequest{}
			_, err = client.Check(tt.ctx, req)
			if w, g := tt.code, status.Code(err); w != g {
				t.Fatalf("status mismatch: want %s, got %s", w, g)
			}

			if !sentry.Flush(time.Second) {
				t.Fatal("sentry.Flush timed out")
			}

			opts := cmp.Options{
				cmpopts.IgnoreFields(
					sentry.Event{},
					"Contexts",
					"EventID",
					"Extra",
					"Platform",
					"Sdk",
					"ServerName",
					"Tags",
					"Timestamp",
				),
				cmpopts.IgnoreFields(
					sentry.Request{},
					"Env",
				),
				cmpopts.IgnoreFields(
					sentry.Exception{},
					"Stacktrace",
				),
			}

			if d := cmp.Diff(tt.events, events, opts); d != "" {
				t.Fatalf("events mismatch (-want +got):\n%s", d)
			}
		})
	}
}

type mockHealthServer struct {
	grpchealth.UnimplementedHealthServer

	handler func(
		context.Context,
		*grpchealth.HealthCheckRequest,
	) (*grpchealth.HealthCheckResponse, error)
}

func (m *mockHealthServer) Check(
	ctx context.Context,
	req *grpchealth.HealthCheckRequest,
) (*grpchealth.HealthCheckResponse, error) {
	return m.handler(ctx, req)
}

func TestReportOnCodes(t *testing.T) {
	for _, tt := range []struct {
		name  string
		err   error
		codes []codes.Code
		want  bool
	}{
		{
			name:  "returns true on code match",
			err:   status.Error(codes.Aborted, ""),
			codes: []codes.Code{codes.Aborted},
			want:  true,
		},
		{
			name:  "returns false on code mismatch",
			err:   status.Error(codes.Aborted, ""),
			codes: []codes.Code{codes.Canceled},
			want:  false,
		},
	} {
		tt := tt

		t.Run(tt.name, func(t *testing.T) {
			if w, g := tt.want, sentrygrpc.ReportOnCodes(tt.codes...)(tt.err); w != g {
				t.Fatalf("want %t, got %t", w, g)
			}
		})
	}
}

func ExampleUnaryServerInterceptor() {
	opts := []sentrygrpc.Option{
		// Reports on OutOfRange or Internal error.
		sentrygrpc.WithReportOn(
			sentrygrpc.ReportOnCodes(
				codes.OutOfRange,
				codes.Internal,
			),
		),
		// Recovers from panic, reports it and returns internal error.
		sentrygrpc.WithRepanic(false),
	}

	// This middleware sets *sentry.Hub to context. You can set user to
	// hub's scope in the later interceptor for example.
	sentry := sentrygrpc.UnaryServerInterceptor(opts...)

	server := grpc.NewServer(grpc.UnaryInterceptor(sentry))
	defer server.Stop()
}
