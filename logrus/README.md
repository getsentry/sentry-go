<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Logrus Hook for Sentry-go SDK

**Go.dev Documentation:** https://pkg.go.dev/github.com/getsentry/sentry-go/logrus  
**Example Usage:** https://github.com/getsentry/sentry-go/tree/master/_examples/logrus

## Installation

```sh
go get github.com/getsentry/sentry-go/logrus
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize Logrus
	logger := logrus.New()

	// Log DEBUG and higher level logs to STDERR
	logger.Level = logrus.DebugLevel
	logger.Out = os.Stderr

	// Initialize Sentry
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "your-public-dsn",
		BeforeSendLog: func(log *sentry.Log) *sentry.Log {
			fmt.Println(log)
			return log
		},
	})
	if err != nil {
		panic(err)
	}

	// send logs on InfoLevel and above, using the client configured by sentry.Init
	logHook := sentrylogrus.NewLogHook(context.Background(), []logrus.Level{
		logrus.InfoLevel,
		logrus.ErrorLevel,
		logrus.FatalLevel,
		logrus.PanicLevel,
	})
	defer logHook.Flush(5 * time.Second)
	logger.AddHook(logHook)

	// Flushes before calling os.Exit(1) when using logger.Fatal
	// (else all defers are not called, and Sentry does not have time to send the log)
	logrus.RegisterExitHandler(func() {
		logHook.Flush(5 * time.Second)
	})

    // Log a InfoLevel entry STDERR which is sent as a log to Sentry
    logger.Infof("Application has started")

	// Log an error to STDERR which is also sent to Sentry as a log
	logger.Errorf("oh no!")

	// Log a fatal error to STDERR, which sends a log to Sentry and terminates the application
	logger.Fatalf("can't continue...")
	
    // Example of logging with attributes
	logger.WithField("user", "test-user").Error("An error occurred")
}
```

## Configuration

The `sentrylogrus` package accepts a context and an array of `logrus.Level`.
The `logrus.Level` array defines which log levels should be sent to Sentry.

In addition, the Hook returned by `sentrylogrus.NewLogHook` can be configured with the following options:

To send logs with a dedicated client, bind it to the context passed to `NewLogHook`:

```go
client, err := sentry.NewClient(sentry.ClientOptions{Dsn: "your-public-dsn"})
if err != nil {
    panic(err)
}
logHook := sentrylogrus.NewLogHook(
    sentry.ContextWithClient(context.Background(), client),
    []logrus.Level{logrus.ErrorLevel},
)
```



- Setting default tags for all events sent to Sentry

```go
sentryHook.AddTags(map[string]string{
    "key": "value",
})
```

- Using a per-entry context for scoped Sentry clients, scopes, or traces

The hook reads the context attached to each log entry. Pass the operation's context with `WithContext` when logs should use an operation-specific client, scope, or trace:

```go
logger.WithContext(ctx).Info("handled request")
```


## Notes

- Always call `Flush` or `FlushWithContext` to ensure all logs are sent to Sentry before program termination
