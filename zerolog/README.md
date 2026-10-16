<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Zerolog Writer for Sentry-Go SDK

**Go.dev Documentation:** https://pkg.go.dev/github.com/getsentry/sentryzerolog  
**Example Usage:** https://github.com/getsentry/sentry-go/tree/master/_examples/zerolog

## Overview

This package provides a writer for the [Zerolog](https://github.com/rs/zerolog) logger, enabling seamless integration with [Sentry](https://sentry.io). With this writer, logs at specific levels can be captured as Sentry events, while others can be added as breadcrumbs for enhanced context.

## Installation

```sh
go get github.com/getsentry/sentry-go/zerolog
```

## Usage

```go
package main

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"github.com/getsentry/sentry-go"
	sentryzerolog "github.com/getsentry/sentry-go/zerolog"
)

func main() {
	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	// Configure Sentry Zerolog Writer
	writer, err := sentryzerolog.New(context.Background(), sentryzerolog.Options{
		Levels:          []zerolog.Level{zerolog.ErrorLevel, zerolog.FatalLevel},
		FlushTimeout:    3 * time.Second,
		WithBreadcrumbs: true,
	})
	if err != nil {
		panic(err)
	}
	defer writer.Close()

	// Initialize Zerolog
	logger := zerolog.New(writer).With().Timestamp().Logger()

	// Example Logs
	logger.Info().Msg("This is an info message")           // Breadcrumb
	logger.Error().Msg("This is an error message")         // Captured as an event
	logger.Fatal().Msg("This is a fatal message")          // Captured as an event and flushes
}
```

## Configuration

The `sentryzerolog.New` function accepts a `context.Context` and a `sentryzerolog.Options` struct.

The writer sends events with the client configured by `sentry.Init`. To send them with a dedicated client instead, bind it to the context with `sentry.ContextWithClient(ctx, client)`.

If the context does not carry a scope, the writer uses its own scope, so breadcrumbs and events never touch the global scope.

The `sentryzerolog.Options` struct allows you to configure the following:

- `Levels`: An array of `zerolog.Level` that defines which log levels should be sent to Sentry.
- `FlushTimeout`: A `time.Duration` that defines how long to wait before flushing events.
- `WithBreadcrumbs`: A `bool` that enables or disables adding logs as breadcrumbs for contextual logging. Non-event logs will appear as breadcrumbs in Sentry.

## Notes

- Always call `Flush` or `FlushWithContext` to ensure all events are sent to Sentry before program termination