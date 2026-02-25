# Sentry Zap Integration

This package provides a [zap](https://github.com/uber-go/zap) `Core` implementation that sends logs to Sentry.

## Installation

```bash
go get github.com/getsentry/sentry-go/zap
```

## Usage

```go
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
        Level: []zapcore.Level{
            zapcore.InfoLevel,
            zapcore.WarnLevel,
            zapcore.ErrorLevel,
        },
        AddCaller: true,
    })

    // Create a zap logger with the Sentry core
    logger := zap.New(sentryCore)

    // Log messages will be sent to Sentry
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
```

## Using with Multiple Cores (Tee)

A common pattern is to log to both the console and Sentry. Use `zapcore.NewTee` to combine multiple cores:

```go
package main

import (
    "context"
    "os"
    "time"

    "github.com/getsentry/sentry-go"
    sentryzap "github.com/getsentry/sentry-go/zap"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

func main() {
    // Initialize Sentry
    sentry.Init(sentry.ClientOptions{
        Dsn:        "your-sentry-dsn",
        EnableLogs: true,
    })
    defer sentry.Flush(2 * time.Second)

    // Create encoder config
    encoderConfig := zap.NewProductionEncoderConfig()
    encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

    // Console core - logs everything to stdout
    consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
    consoleCore := zapcore.NewCore(
        consoleEncoder,
        zapcore.AddSync(os.Stdout),
        zapcore.DebugLevel,
    )

    // Sentry core - only sends Warn and above to Sentry
    ctx := context.Background()
    sentryCore := sentryzap.NewSentryCore(ctx, sentryzap.Option{
        Level: []zapcore.Level{
            zapcore.WarnLevel,
            zapcore.ErrorLevel,
        },
    })

    // Combine cores
    combinedCore := zapcore.NewTee(consoleCore, sentryCore)

    // Create logger with caller info
    logger := zap.New(combinedCore, zap.AddCaller())

    // Debug goes to console only
    logger.Debug("Debug message - console only")

    // Warn goes to both console and Sentry
    logger.Warn("Warning message - console and Sentry")
}
```

## Configuration Options

### Option struct

| Field | Type | Description | Default |
|-------|------|-------------|---------|
| `Level` | `[]zapcore.Level` | Zap levels to capture and send to Sentry | All levels (Debug through Fatal) |
| `AddCaller` | `bool` | Include caller info (file, line, function) | `false` |
| `FlushTimeout` | `time.Duration` | How long to wait when syncing/flushing | 5 seconds |

## Context and Tracing

The Sentry core respects the context passed during initialization. If you have Sentry tracing enabled, logs will be associated with the current span.

### Option 1: Pass context when creating the core

```go
ctx := context.Background()

// Start a transaction
span := sentry.StartSpan(ctx, "operation.name")
defer span.Finish()

// Create logger with the span's context
ctx = span.Context()
sentryCore := sentryzap.NewSentryCore(ctx, sentryzap.Option{})
logger := zap.New(sentryCore)

// This log will be associated with the transaction
logger.Info("Processing started")
```

### Option 2: Use the Context() helper for dynamic trace propagation

If you need to propagate different contexts for different log calls, use the `Context()` helper with `logger.With()`:

```go
// Create logger with base context
sentryCore := sentryzap.NewSentryCore(context.Background(), sentryzap.Option{})
logger := zap.New(sentryCore)

// Start a transaction
span := sentry.StartTransaction(ctx, "operation.name")
defer span.Finish()

// Create a logger scoped to this transaction
scopedLogger := logger.With(sentryzap.Context(span.Context()))

// These logs will be associated with the transaction
scopedLogger.Info("Processing started")
scopedLogger.Info("Processing completed")
```

## Notes

- This integration only sends logs to Sentry (not events/errors). For error reporting, use the main `sentry-go` package.
- Ensure `EnableLogs: true` is set in your Sentry client options.
- Call `sentry.Flush()` before your application exits to ensure all logs are sent.
