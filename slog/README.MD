<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Integration for slog

**Go.dev Documentation:** https://pkg.go.dev/github.com/getsentry/sentry-go/slog
**Example Usage:** https://github.com/getsentry/sentry-go/tree/master/_examples/slog

---

## Installation

```sh
go get github.com/getsentry/sentry-go/slog

```

## Usage

```go
package main

import (
	"context"
	"log/slog"

	"github.com/getsentry/sentry-go"
	slogSentry "github.com/getsentry/sentry-go/slog"
)

func main() {
	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
		Debug: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(5 * time.Second)

	// Set up slog with Sentry handler
	handler := slogSentry.Option{
		Level: slog.LevelError, // Minimum log level
		AddSource: true,        // Include file/line source info
	}.NewSentryHandler()
	logger := slog.New(handler)

	// Example logging
	logger.Info("This will not be sent to Sentry")
	logger.Error("An error occurred", "user", "test-user")
}
```

## Configuration

The slog-sentry package offers several options to customize how logs are handled and sent to Sentry. These are specified through the Option struct:

- `Level`: Minimum log level to send to Sentry. Defaults to `slog.LevelDebug`.

- `Hub`: Custom Sentry hub to use; defaults to the current Sentry hub if not set.

- `Converter`: Custom function to transform logs into Sentry events (default is DefaultConverter).

- `AttrFromContext`: Functions to extract additional attributes from the context.

- `AddSource`: Include file/line source info in Sentry events. Defaults to `false`.

- `ReplaceAttr`:  Allows modification or filtering of attributes before sending to Sentry.


### Example Customization

```go
handler := slogSentry.Option{
	Level: slog.LevelWarn,
	Converter: func(addSource bool, replaceAttr func([]string, slog.Attr) slog.Attr, attrs []slog.Attr, groups []string, record *slog.Record, hub *sentry.Hub) *sentry.Event {
		// Custom conversion logic
		return &sentry.Event{
			Message: record.Message,
		}
	},
	AddSource: true,
}.NewSentryHandler()
```

## Notes

- Always call Flush to ensure all events are sent to Sentry before program termination
