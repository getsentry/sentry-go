package main

import (
	"context"
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
	loggerWithVersion := sentry.NewLogger(ctx)
	// Setting  attributes (these persist across log calls)
	loggerWithVersion.SetAttributes(
		attribute.String("permanent.version", "1.0.0"),
	)
	// Add attributes to log entry
	loggerWithVersion.Info().WithCtx(ctx).
		String("key.string", "value").
		Int("key.int", 42).
		Bool("key.bool", true).
		// don't forget to call Emit to send the logs to Sentry
		Emitf("Message with parameters %d and %d", 1, 2)

	logger := sentry.NewLogger(ctx)
	// you can also use Attributes for setting many attributes in the log entry (these are temporary)
	logger.Info().
		Attributes(
			attribute.String("key.temp.string", "value"),
			attribute.Int("key.temp.int", 42),
		).Emit("doesn't contain permanent.version")
}
