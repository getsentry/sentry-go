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
	sentryslog "github.com/getsentry/sentry-go/slog"
)

func main() {
	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
		Debug: true,
        EnableLogs: true, 
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(5 * time.Second)

	ctx := context.Background()
	handler := sentryslog.Option{
		EventLevel: []slog.Level{slog.LevelError, sentryslog.LevelFatal}, // Only Error and Fatal as events
		LogLevel:   []slog.Level{slog.LevelWarn, slog.LevelInfo},         // Only Warn and Info as logs
	}.NewSentryHandler(ctx)
    logger := slog.New(handler)

	// Example logging
	logger.Info("This will be sent to sentry as a Log entry")
	logger.Error("An error occurred", "user", "test-user") // this will be sent as an Event
	// These will be ignored
	logger.Debug("This will be ignored")
}
```

## Configuration

The slog-sentry package offers several options to customize how logs are handled and sent to Sentry. These are specified through the Option struct:

- `EventLevel`: Slice of specific levels to send `Events` to Sentry. Defaults to `[]slog.Level{slog.LevelError, LevelFatal}`.

- `LogLevel`: Slice of specific levels to send `Log` entries to Sentry. Defaults to `[]slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, LevelFatal}`.

- `Hub`: Custom Sentry hub to use; defaults to the current Sentry hub if not set.

- `Converter`: Custom function to transform logs into Sentry events (default is DefaultConverter).

- `AttrFromContext`: Functions to extract additional attributes from the context.

- `AddSource`: Include file/line source info in Sentry events. Defaults to `false`.

- `ReplaceAttr`:  Allows modification or filtering of attributes before sending to Sentry.


### Example Customization

```go
handler := slogSentry.Option{
	EventLevel: slog.LevelWarn,
	Converter: func(addSource bool, replaceAttr func([]string, slog.Attr) slog.Attr, attrs []slog.Attr, groups []string, record *slog.Record, hub *sentry.Hub) *sentry.Event {
		// Custom conversion logic
		return &sentry.Event{
			Message: record.Message,
		}
	},
	AddSource: true,
}.NewSentryHandler()
```

### Backwards Compatibility

The old `Level` field is Deprecated but still works and will be converted to a slice of all levels starting from the minimum level:

```go
// Old way (still works)
handler := sentryslog.Option{
    Level: slog.LevelWarn, // Will be converted to EventLevel: [Warn, Error, Fatal]
}.NewSentryHandler(ctx)

// New way (preferred)
handler := sentryslog.Option{
    EventLevel: []slog.Level{slog.LevelWarn, slog.LevelError, sentryslog.LevelFatal},
    LogLevel:   []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError, sentryslog.LevelFatal},
}.NewSentryHandler(ctx)
```
## Notes

- Always call `Flush` or `FlushWithContext` to ensure all events are sent to Sentry before program termination
