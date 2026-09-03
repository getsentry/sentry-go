// nolint // Don't lint example code.
package sentryhttp_test

import (
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

// For a longer and executable example, see
// https://github.com/getsentry/sentry-go/tree/master/_examples/http.
func Example() {
	// Initialize the Sentry SDK once in the main function.
	// sentry.Init(...)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Capture with the request context so the request-specific scope and
		// trace are applied to the event.
		_, err := http.Get("example.com")
		if err != nil {
			sentry.CaptureException(r.Context(), err)
		}
	})

	// Wrap the default mux with Sentry to capture panics and report errors.
	//
	// Alternatively, you can also wrap individual handlers if you need to use
	// different options for different parts of your app.
	handler := sentryhttp.New(sentryhttp.Options{}).Handle(http.DefaultServeMux)

	server := http.Server{
		Addr:              ":0",
		ReadHeaderTimeout: 3 * time.Second,
		Handler:           handler,
	}
	server.ListenAndServe()
}
