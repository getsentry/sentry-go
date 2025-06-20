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
import (
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/getsentry/sentry-go"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
)

func main() {
	// Initialize Logrus
	logger := logrus.New()
	logger.Level = logrus.DebugLevel
	logger.Out = os.Stderr

	// Define log levels to send to Sentry
	sentryLevels := []logrus.Level{logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel}

	// Initialize Sentry
	sentryHook, err := sentrylogrus.New(sentryLevels, sentry.ClientOptions{
		Dsn: "your-public-dsn",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			fmt.Println(event) // Optional debugging
			return event
		},
		Debug: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentryHook.Flush(5 * time.Second)
	logger.AddHook(sentryHook)

	// Example logging
	logger.WithField("user", "test-user").Error("An error occurred")

	// Ensure all events are flushed before exit
	logrus.RegisterExitHandler(func() { sentryHook.Flush(5 * time.Second) })
}
```

## Configuration

The `sentrylogrus` package accepts an array of `logrus.Level` and a struct of `sentry.ClientOptions` that allows you to configure how the hook will behave.
The `logrus.Level` array defines which log levels should be sent to Sentry.

In addition, the Hook returned by `sentrylogrus.New` can be configured with the following options:

- Fallback Functionality: Configure a fallback for handling errors during log transmission.

```go
sentryHook.Fallback = func(entry *logrus.Entry, err error) {
    // Handle error
}
```

- Setting default tags for all events sent to Sentry

```go
sentryHook.AddTags(map[string]string{
    "key": "value",
})
```

- Using `hubProvider` for Scoped Sentry Hubs

The hubProvider allows you to configure the Sentry hook to use a custom Sentry hub. This can be particularly useful when you want to scope logs to specific goroutines or operations, enabling more precise grouping and context in Sentry.

You can set a custom hubProvider function using the SetHubProvider method:

```go
sentryHook.SetHubProvider(func() *sentry.Hub {
    // Create or return a specific Sentry hub
    return sentry.NewHub(sentry.GetCurrentHub().Client(), sentry.NewScope())
})
```

This ensures that logs from specific contexts or threads use the appropriate Sentry hub and scope.


## Notes

- Always call `Flush` or `FlushWithContext` to ensure all events are sent to Sentry before program termination

