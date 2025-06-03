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
		EnableLogs: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	logger := sentry.NewLogger(ctx)

	// You can use the logger like [fmt.Print]
	logger.Info(ctx, "Expecting ", 2, " params")
	// or like [fmt.Printf]
	logger.Infof(ctx, "format: %v", "value")

	// Additionally, you can also set attributes on the log like this
	logger.SetAttributes(
		attribute.Int("key.int", 42),
		attribute.Bool("key.boolean", true),
		attribute.Float64("key.float", 42.4),
		attribute.String("key.string", "string"),
	)
	logger.Warnf(ctx, "I have params %v and attributes", "example param")
}
