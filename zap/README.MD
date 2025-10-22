<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Zap Core for Sentry-Go SDK

**Go.dev Documentation:** [https://pkg.go.dev/github.com/getsentry/sentryzap](https://pkg.go.dev/github.com/getsentry/sentryzap)  
**Example Usage:** [https://github.com/getsentry/sentry-go/tree/master/_examples/zap](https://github.com/getsentry/sentry-go/tree/master/_examples/zap)

## Overview

This package provides a core for the [Zap](https://github.com/uber-go/zap) logger, enabling seamless integration with [Sentry](https://sentry.io). With this core, logs at specific levels can be captured as Sentry events, while others can be added as breadcrumbs for enhanced context.

## Installation

```sh
go get github.com/getsentry/sentry-go/zap
```

## Usage

```go
package main

import (
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/zap"
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
```

## Configuration

The `sentryzap.NewCore` function accepts a `sentryzap.Configuration` struct, which allows for the following configuration options:

- Tags: A map of key-value pairs to add as tags to every event sent to Sentry.
- LoggerNameKey: Specifies the field key used to represent the logger name in the Sentry event. If empty, this feature is disabled.
- DisableStacktrace: If true, stack traces are not included in events sent to Sentry.
- Level: The minimum severity level for logs to be sent to Sentry.
- EnableBreadcrumbs: If true, logs below the event level are added as breadcrumbs to Sentry.
- BreadcrumbLevel: The minimum severity level for breadcrumbs to be recorded.
- Note: BreadcrumbLevel must be lower than or equal to Level.
- MaxBreadcrumbs: The maximum number of breadcrumbs to retain (default: 100).
- FlushTimeout: The maximum duration to wait for events to flush when calling Sync() (default: 3 seconds).
- Hub: Overrides the default Sentry hub.
- FrameMatcher: A function to filter stack trace frames.

## Notes

- Always call sentry.Flush to ensure all events are sent to Sentry before program termination.
- Use sentryzap.AttachCoreToLogger to attach the Sentry core to your existing logger seamlessly.