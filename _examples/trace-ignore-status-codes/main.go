package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

func main() {
	// Initialize Sentry with TraceIgnoreStatusCodes configuration
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "", // Replace with your DSN
		Debug:            true,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		// Configure which HTTP status codes should not be traced
		TraceIgnoreStatusCodes: []int{404, 403}, // Ignore 404 Not Found and 403 Forbidden
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	defer sentry.Flush(2 * time.Second)

	// Create a Sentry-instrumented HTTP handler
	sentryHandler := sentryhttp.New(sentryhttp.Options{})

	http.HandleFunc("/", sentryHandler.HandleFunc(homeHandler))
	http.HandleFunc("/users/", sentryHandler.HandleFunc(usersHandler))

	fmt.Println("Server starting on :8080")
	fmt.Println("Try these endpoints:")
	fmt.Println("  GET /           - Returns 200 OK (will be traced)")
	fmt.Println("  GET /users/123  - Returns 200 OK (will be traced)")
	fmt.Println("  GET /nonexistent - Returns 404 Not Found (will NOT be traced)")

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// This will return 404 and won't be traced due to our configuration
		http.NotFound(w, r)
		return
	}

	// Add custom data to the transaction
	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "home")
		span.SetData("custom_data", "This is the home page")
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Welcome to the home page! This 200 response will be traced.\n")
}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	// Add custom data to the transaction
	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "users")
		span.SetData("user_id", r.URL.Path[7:]) // Extract user ID from path
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "User profile page. This 200 response will be traced.\n")
}
