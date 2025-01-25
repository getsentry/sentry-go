package main

import (
	"time"

	"github.com/getsentry/sentry-go"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Initialize Sentry
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: "your-public-dsn",
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	// Configure Sentry Zap Core
	sentryCore, err := sentryzap.NewCore(
		sentryzap.Configuration{
			Level:             zapcore.ErrorLevel,
			BreadcrumbLevel:   zapcore.InfoLevel,
			EnableBreadcrumbs: true,
			FlushTimeout:      3 * time.Second,
		},
		sentryzap.NewSentryClientFromClient(client),
	)
	if err != nil {
		panic(err)
	}

	// Create a logger with Sentry Core
	logger := sentryzap.AttachCoreToLogger(sentryCore, zap.NewExample())

	// Example Logs
	logger.Info("This is an info message")   // Breadcrumb
	logger.Error("This is an error message") // Captured as an event
	logger.Fatal("This is a fatal message")  // Captured as an event and flushes
}
