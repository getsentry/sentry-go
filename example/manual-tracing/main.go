// This is an example program that makes an HTTP request and prints response
// headers. Whenever a request fails, the error is reported to Sentry.
//
// Try it by running:
//
// 	go run main.go
//
// To actually report events to Sentry, set the DSN either by editing the
// appropriate line below or setting the environment variable SENTRY_DSN to
// match the DSN of your Sentry project.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/x/sentryhttp"
)

func run() error {
	err := sentry.Init(sentry.ClientOptions{
		// Either set your DSN here or set the SENTRY_DSN environment variable.
		Dsn: "",
		// Enable printing of SDK debug messages.
		// Useful when getting started or trying to figure something out.
		Debug: true,
		// Specify either TracesSampleRate or TracesSampler to enable tracing.
		TracesSampleRate: 1.0,
		TracesSampler: sentry.TracesSamplerFunc(func(ctx sentry.SamplingContext) bool {
			// Use the data stored in ctx to make custom sampling decisions.

			// TODO: expose a few standard samplers that can be composed/reused:
			// - FixedRate
			// - ParentBased
			// - DeterministicFraction (of TraceID or ParentBased+SpanID)

			return true
		}),
	})
	if err != nil {
		return fmt.Errorf("sentry.Init: %w", err)
	}
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(2 * time.Second)

	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "It works!")
		time.Sleep(80 * time.Millisecond) // simulate network latency
	}))

	span := sentry.StartSpan(context.Background(), "top", sentry.WithTransactionName("Example Transaction"))
	defer func() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(span); err != nil {
			panic(err)
		}
	}()
	defer span.Finish()
	child1 := span.StartChild("child1")
	time.Sleep(20 * time.Millisecond)
	grandchild1 := child1.StartChild("grandchild1")
	time.Sleep(100 * time.Millisecond)
	grandchild1.Finish()
	child1.Finish()
	child2 := span.StartChild("child2")
	// client := http.Client{Transport: sentryhttp.NewTransport(http.DefaultTransport)}
	// req, err := http.NewRequestWithContext(child2.Context(), "GET", "/", nil)
	// iferr...
	// resp, err := client.Do(req)
	resp, err := sentryhttp.Get(child2.Context(), testServer.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := httputil.DumpResponse(resp, true)
	if err != nil {
		return err
	}
	fmt.Printf("%s", b)
	time.Sleep(50 * time.Millisecond)
	child2.Finish()

	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
