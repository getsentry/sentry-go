package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"grpcdemo/cmd/server/examplepb"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const grpcServerAddress = "localhost:50051"

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

	// Create a connection to the gRPC server with Sentry interceptors
	conn, err := grpc.NewClient(
		grpcServerAddress,
		grpc.WithTransportCredentials(insecure.NewCredentials()), // Use TLS in production
		grpc.WithUnaryInterceptor(sentrygrpc.UnaryClientInterceptor()),
		grpc.WithStreamInterceptor(sentrygrpc.StreamClientInterceptor()),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %s", err)
	}
	defer conn.Close()

	// Create a client for the ExampleService
	client := examplepb.NewExampleServiceClient(conn)

	// Perform Unary call
	fmt.Println("Performing Unary Call:")
	unaryExample(client)

	// Perform Streaming call
	fmt.Println("\nPerforming Streaming Call:")
	streamExample(client)
}

func unaryExample(client examplepb.ExampleServiceClient) {
	ctx := context.Background()

	// Add metadata to the context
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"custom-header", "value",
	))

	req := &examplepb.ExampleRequest{
		Message: "Hello, server!", // Change to "error" to simulate an error
	}

	res, err := client.UnaryExample(ctx, req)
	if err != nil {
		fmt.Printf("Unary Call Error: %v\n", err)
		sentry.CaptureException(err)
		return
	}

	fmt.Printf("Unary Response: %s\n", res.Message)
}

func streamExample(client examplepb.ExampleServiceClient) {
	ctx := context.Background()

	// Add metadata to the context
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs(
		"streaming-header", "stream-value",
	))

	stream, err := client.StreamExample(ctx)
	if err != nil {
		fmt.Printf("Failed to establish stream: %v\n", err)
		sentry.CaptureException(err)
		return
	}

	// Send multiple messages in the stream
	messages := []string{"Message 1", "Message 2", "error", "Message 4"}
	for _, msg := range messages {
		err := stream.Send(&examplepb.ExampleRequest{Message: msg})
		if err != nil {
			fmt.Printf("Stream Send Error: %v\n", err)
			sentry.CaptureException(err)
			return
		}
	}

	// Close the stream for sending
	stream.CloseSend()

	// Receive responses from the server
	for {
		res, err := stream.Recv()
		if err != nil {
			fmt.Printf("Stream Recv Error: %v\n", err)
			sentry.CaptureException(err)
			break
		}
		fmt.Printf("Stream Response: %s\n", res.Message)
	}
}
