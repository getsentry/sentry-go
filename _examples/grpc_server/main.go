package main

import (
	"context"
	"fmt"
	"grpcdemo/cmd/server/examplepb"
	"log"
	"net"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const grpcPort = ":50051"

// ExampleServiceServer is the server implementation for the ExampleService.
type ExampleServiceServer struct {
	examplepb.UnimplementedExampleServiceServer
}

// UnaryExample handles unary gRPC requests.
func (s *ExampleServiceServer) UnaryExample(ctx context.Context, req *examplepb.ExampleRequest) (*examplepb.ExampleResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	fmt.Printf("Received Unary Request: %v\nMetadata: %v\n", req.Message, md)

	// Simulate an error for demonstration
	if req.Message == "error" {
		return nil, fmt.Errorf("simulated unary error")
	}

	return &examplepb.ExampleResponse{Message: fmt.Sprintf("Hello, %s!", req.Message)}, nil
}

// StreamExample handles bidirectional streaming gRPC requests.
func (s *ExampleServiceServer) StreamExample(stream examplepb.ExampleService_StreamExampleServer) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			fmt.Printf("Stream Recv Error: %v\n", err)
			return err
		}

		fmt.Printf("Received Stream Message: %v\n", req.Message)

		if req.Message == "error" {
			return fmt.Errorf("simulated stream error")
		}

		err = stream.Send(&examplepb.ExampleResponse{Message: fmt.Sprintf("Echo: %s", req.Message)})
		if err != nil {
			fmt.Printf("Stream Send Error: %v\n", err)
			return err
		}
	}
}

func main() {
	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "",
		TracesSampleRate: 1.0,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	defer sentry.Flush(2 * time.Second)

	// Create a new gRPC server with Sentry interceptors
	server := grpc.NewServer(
		grpc.UnaryInterceptor(sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{
			Repanic:            true,
			CaptureRequestBody: true,
		})),
		grpc.StreamInterceptor(sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{
			Repanic: true,
		})),
	)

	// Register the ExampleService
	examplepb.RegisterExampleServiceServer(server, &ExampleServiceServer{})

	// Start the server
	listener, err := net.Listen("tcp", grpcPort)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", grpcPort, err)
	}

	fmt.Printf("gRPC server is running on %s\n", grpcPort)
	if err := server.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
