package main

import (
	"fmt"
	"log"
	"time"

	"log/slog"

	"github.com/getsentry/sentry-go"
	sentryslog "github.com/getsentry/sentry-go/slog"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:           "https://4d0ead95fb645aaab4fc95d21aaa6de1@o447951.ingest.us.sentry.io/4506954545758208",
		EnableTracing: false,
	})
	if err != nil {
		log.Fatal(err)
	}

	defer sentry.Flush(2 * time.Second)

	logger := slog.New(sentryslog.Option{Level: slog.LevelDebug}.NewSentryHandler())
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
