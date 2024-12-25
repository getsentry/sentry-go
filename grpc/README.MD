<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry gRPC Interceptor for Sentry-go SDK

**go.dev:** [https://pkg.go.dev/github.com/getsentry/sentry-go/grpc](https://pkg.go.dev/github.com/getsentry/sentry-go/grpc)

**Example:** https://github.com/getsentry/sentry-go/tree/master/_examples/grpc


## Installation

```sh
go get github.com/getsentry/sentry-go/grpc
```

## Server-Side Usage

```go
import (
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
)

func main() {
	// Initialize Sentry
	if err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
	}); err != nil {
		fmt.Printf("Sentry initialization failed: %v\n", err)
	}

	// Create gRPC server with Sentry interceptors
	server := grpc.NewServer(
		sentrygrpc.UnaryServerInterceptor(sentrygrpc.ServerOptions{
			Repanic:         true,
			WaitForDelivery: true,
		}),
		sentrygrpc.StreamServerInterceptor(sentrygrpc.ServerOptions{
			Repanic:         true,
			WaitForDelivery: true,
		}),
	)

	// Register reflection for debugging
	reflection.Register(server)

	// Start the server
	listener, err := net.Listen("tcp", ":50051")
	if err != nil {
		sentry.CaptureException(err)
		fmt.Printf("Failed to listen: %v\n", err)
		return
	}

	fmt.Println("Server running...")
	if err := server.Serve(listener); err != nil {
		sentry.CaptureException(err)
	}
}
```


## Client-Side Usage

```go
import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/getsentry/sentry-go"
	sentrygrpc "github.com/getsentry/sentry-go/grpc"
)

func main() {
	// Initialize Sentry
	if err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
	}); err != nil {
		fmt.Printf("Sentry initialization failed: %v\n", err)
	}

	// Create gRPC client with Sentry interceptors
	conn, err := grpc.Dial(
		"localhost:50051",
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(sentrygrpc.UnaryClientInterceptor(sentrygrpc.ClientOptions{})),
		grpc.WithStreamInterceptor(sentrygrpc.StreamClientInterceptor(sentrygrpc.ClientOptions{})),
	)
	if err != nil {
		sentry.CaptureException(err)
		fmt.Printf("Failed to connect: %v\n", err)
		return
	}
	defer conn.Close()

	client := NewYourServiceClient(conn)

	// Make a request
	_, err = client.YourMethod(context.Background(), &YourRequest{})
	if err != nil {
		sentry.CaptureException(err)
		fmt.Printf("Error calling method: %v\n", err)
	}
}
```

## Configuration

Both the server and client interceptors accept options for customization:

### Server Options

```go
type ServerOptions struct {
	// Repanic determines whether the application should re-panic after recovery.
	Repanic bool

	// WaitForDelivery determines if the interceptor should block until events are sent to Sentry.
	WaitForDelivery bool

	// Timeout sets the maximum duration for Sentry event delivery.
	Timeout time.Duration

	// ReportOn defines the conditions under which errors are reported to Sentry.
	ReportOn func(error) bool

	// CaptureRequestBody determines whether to capture and send request bodies to Sentry.
	CaptureRequestBody bool

	// OperationName overrides the default operation name (grpc.server).
	OperationName string
}
```

### Client Options

```go
type ClientOptions struct {
	// ReportOn defines the conditions under which errors are reported to Sentry.
	ReportOn func(error) bool

	// OperationName overrides the default operation name (grpc.client).
	OperationName string
}
```

## Notes

- The interceptors automatically create and manage a Sentry *Hub for each gRPC request or stream.
- Use the Sentry SDK’s context-based APIs to capture exceptions and add additional context.
- Ensure you handle the context correctly to propagate tracing information across requests.
