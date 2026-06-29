package main

import (
	"fmt"
	"net/http"
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

	// send logs on InfoLevel and above
	logHook, err := sentrylogrus.NewLogHook(
		[]logrus.Level{
			logrus.InfoLevel,
			logrus.ErrorLevel,
			logrus.FatalLevel,
			logrus.PanicLevel,
		},
		sentry.ClientOptions{
			Dsn: "your-public-dsn",
			BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
				if hint.Context != nil {
					if req, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
						// You have access to the original Request
						fmt.Println(req)
					}
				}
				fmt.Println(event)
				return event
			},
			// need to have logs enabled
			Debug:            true,
			AttachStacktrace: true,
		})

	if err != nil {
		panic(err)
	}
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
}
