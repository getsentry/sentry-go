package main

import (
	"context"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:        "",
		EnableLogs: true, // you need to have EnableLogs set to true
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	loggerWithAttrs := sentry.NewLogger(ctx)
	// Attaching permanent attributes on the logger.
	loggerWithAttrs.SetAttributes(
		attribute.String("version", "1.0.0"),
	)

	// It's also possible to attach attributes on the [LogEntry] itself.
	loggerWithAttrs.Info().
		String("key.string", "value").
		Int("key.int", 42).
		Bool("key.bool", true).
		// don't forget to call Emit to send the logs to Sentry
		Emitf("Message with parameters %d and %d", 1, 2)

	// The [LogEntry] can also be precompiled, if you don't want to set the same attributes multiple times
	logEntry := loggerWithAttrs.Info().Int("int", 1)
	// And then call Emit multiple times
	logEntry.Emit("once")
	logEntry.Emit("twice")

	// You can also create different loggers with different precompiled attributes
	logger := sentry.NewLogger(ctx)
	logger.Info().
		Emit("doesn't contain version") // this log does not contain the version attribute
}

type MyHandler struct {
	logger sentry.Logger
}

// ServeHTTP example of a handler
// To correlate logs with transactions, [context.Context] needs to be passed to the [LogEntry] with the [WithCtx] func.
// Assuming you are using a Sentry tracing integration.
func (h MyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// By using [WithCtx] the log entry will be associated with the transaction from the request
	h.logger.Info().WithCtx(ctx).Emit("log inside handler")
	w.WriteHeader(http.StatusOK)
}
