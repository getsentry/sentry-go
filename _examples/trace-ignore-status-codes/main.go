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
		// Each element can be a single code {code} or a range {min, max}
		TraceIgnoreStatusCodes: [][]int{{404}, {500, 599}}, // Ignore 404 and server errors 500-599
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}

	defer sentry.Flush(2 * time.Second)

	// Create a Sentry-instrumented HTTP handler
	sentryHandler := sentryhttp.New(sentryhttp.Options{})

	http.HandleFunc("/", sentryHandler.HandleFunc(homeHandler))
	http.HandleFunc("/users/", sentryHandler.HandleFunc(usersHandler))
	http.HandleFunc("/forbidden", sentryHandler.HandleFunc(forbiddenHandler))
	http.HandleFunc("/error", sentryHandler.HandleFunc(errorHandler))

	fmt.Println("Server starting on :8080")
	fmt.Println("Try these endpoints:")
	fmt.Println("  GET /             - Returns 200 OK (will be traced)")
	fmt.Println("  GET /users/123     - Returns 200 OK (will be traced)")
	fmt.Println("  GET /nonexistent  - Returns 404 Not Found (will NOT be traced - matches {404})")
	fmt.Println("  GET /forbidden    - Returns 403 Forbidden (will be traced)")
	fmt.Println("  GET /error        - Returns 500 Internal Server Error (will NOT be traced - in range {500, 599})")

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		// This will return 404 and won't be traced due to our configuration (matches {404})
		http.NotFound(w, r)
		return
	}

	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "home")
		span.SetData("custom_data", "This is the home page")
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Welcome to the home page! This 200 response will be traced.\n")
}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "users")
		span.SetData("user_id", r.URL.Path[7:])
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "User profile page. This 200 response will be traced.\n")
}

func forbiddenHandler(w http.ResponseWriter, r *http.Request) {
	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "forbidden")
		span.SetData("reason", "Access denied")
	}

	w.WriteHeader(http.StatusForbidden)
	fmt.Fprintf(w, "Access forbidden. This 403 response will be traced.\n")
}

func errorHandler(w http.ResponseWriter, r *http.Request) {
	if span := sentry.SpanFromContext(r.Context()); span != nil {
		span.SetTag("endpoint", "error")
		span.SetData("error_type", "simulated_server_error")
	}

	w.WriteHeader(http.StatusInternalServerError)
	fmt.Fprintf(w, "Internal server error. This 500 response will NOT be traced (in range 500-599).\n")
}
