package main

import (
	"context"
	"errors"
	"time"

	"github.com/getsentry/sentry-go"
	sentryzap "github.com/getsentry/sentry-go/zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Initialize Sentry with logs enabled
	err := sentry.Init(sentry.ClientOptions{
		Dsn:        "your-sentry-dsn",
		EnableLogs: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	// Create the Sentry core
	ctx := context.Background()
	sentryCore := sentryzap.NewSentryCore(ctx, sentryzap.Option{
		Level: []zapcore.Level{ // define capture levels
			zapcore.InfoLevel,
			zapcore.WarnLevel,
			zapcore.ErrorLevel,
		},
		AddCaller: true,
	})

	logger := zap.New(sentryCore)
	logger.Info("Application started",
		zap.String("version", "1.0.0"),
		zap.String("environment", "production"),
	)

	logger.Warn("High memory usage",
		zap.Float64("usage_percent", 85.5),
	)

	logger.Error("Database connection failed",
		zap.Error(errors.New("connection timeout")),
		zap.String("host", "db.example.com"),
	)
}
