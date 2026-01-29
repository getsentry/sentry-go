package main

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Initialize Sentry with logs enabled
	err := sentry.Init(sentry.ClientOptions{
		Dsn:        "", // Set your Sentry DSN here
		EnableLogs: true,
		Debug:      true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(5 * time.Second)

	ctx := context.Background()

	// Start a transaction
	span := sentry.StartTransaction(ctx, "zap-example")
	defer span.Finish()

	// Create logger with the transaction context
	sentryCore := sentryzap.NewSentryCore(context.Background(), sentryzap.Option{
		Level: []zapcore.Level{zapcore.InfoLevel},
	})
	logger := zap.New(sentryCore)

	// Use the Context() helper to associate logs with the transaction
	scopedLogger := logger.With(sentryzap.Context(span.Context()))
	scopedLogger.Info("Transaction started")
	time.Sleep(100 * time.Millisecond)
	scopedLogger.Info("Processing data",
		zap.Int("records", 100),
	)
	time.Sleep(100 * time.Millisecond)
	scopedLogger.Info("Transaction completed")
}
