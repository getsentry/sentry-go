package main

import (
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	internalHttp "github.com/getsentry/sentry-go/internal/http"
)

func main() {
	// Register the telemetry transport to enable telemetry features
	internalHttp.RegisterTelemetryTransport()

	// Initialize Sentry with telemetry buffers enabled
	err := sentry.Init(sentry.ClientOptions{
		Dsn:                    "https://your-dsn@sentry.io/project-id",
		EnableTelemetryBuffers: true,
		Debug:                  true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	// Now telemetry will use the AsyncTransport from internal/http
	// which implements protocol.TelemetryTransport for efficient buffering
	fmt.Println("Sentry initialized with telemetry support!")

	// Send some test events
	sentry.CaptureMessage("Hello with telemetry!")
	sentry.CaptureException(fmt.Errorf("test error"))

	fmt.Println("Events sent using telemetry transport!")
}
