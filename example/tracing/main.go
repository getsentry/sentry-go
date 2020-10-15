// This is an example program that demonstrates how to instrument HTTP clients
// and servers to report traces to Sentry.
//
// It starts an HTTP server on a random free port and makes a request using an
// HTTP client and prints a URL to the generated trace.
//
// Try it by running:
//
// 	SENTRY_DSN=... go run .
//
// To actually report events to Sentry, set the DSN either by editing the
// appropriate line below or setting the environment variable SENTRY_DSN to
// match the DSN of your Sentry project.
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
	"golang.org/x/sync/errgroup"
)

func run() error {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug: true,
	})
	if err != nil {
		return fmt.Errorf("sentry.Init: %w", err)
	}
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(2 * time.Second)

	var g errgroup.Group
	g.Go(server)
	g.Go(client)
	return g.Wait()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
