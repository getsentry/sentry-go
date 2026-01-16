// This is an example program that demonstrates Sentry Go SDK integration
// with Spotlight for local development debugging.
//
// Try it by running:
//
//	go run main.go
//
// To actually report events to Sentry, set the DSN either by editing the
// appropriate line below or setting the environment variable SENTRY_DSN to
// match the DSN of your Sentry project.
//
// Before running this example, make sure Spotlight is running:
//
//	npm install -g @spotlightjs/spotlight
//	spotlight
//
// Then open http://localhost:8969 in your browser to see the Spotlight UI.
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug: true,
		// Enable Spotlight for local debugging.
		Spotlight: true,
		// Enable tracing to see performance data in Spotlight.
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(2 * time.Second)

	log.Println("Sending sample events to Spotlight...")

	// Capture a simple message
	sentry.CaptureMessage("Hello from Spotlight!")

	// Capture an exception
	sentry.CaptureException(errors.New("example error for Spotlight debugging"))

	// Capture an event with additional context
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("environment", "development")
		scope.SetLevel(sentry.LevelWarning)
		scope.SetContext("example", map[string]interface{}{
			"feature": "spotlight_integration",
			"version": "1.0.0",
		})
		sentry.CaptureMessage("Event with additional context")
	})

	// Performance monitoring example
	span := sentry.StartSpan(context.Background(), "example.operation")
	defer span.Finish()

	span.SetData("example", "data")
	childSpan := span.StartChild("child.operation")
	// Simulate some work
	time.Sleep(100 * time.Millisecond)
	childSpan.Finish()

	log.Println("Events sent! Check your Spotlight UI at http://localhost:8969")
}
