package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"log/slog"

	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:           "",
		EnableTracing: false,
	})
	if err != nil {
		log.Fatal(err)
	}

	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	handler := sentryslog.Option{
		LogLevel: []slog.Level{slog.LevelWarn, slog.LevelInfo, slog.LevelError, sentryslog.LevelFatal},
	}.NewSentryHandler(ctx)
	logger := slog.New(handler)
	logger = logger.With("release", "v1.0.0")

	logger.
		With(
			slog.Group("user",
				slog.String("id", "user-123"),
				slog.Time("created_at", time.Now()),
			),
		).
		With("environment", "dev").
		With("error", fmt.Errorf("an error")).
		Error("a message")
}
