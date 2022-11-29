package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/getsentry/sentry-go"
	sentrylogrus "github.com/getsentry/sentry-go/logrus"
)

func main() {
	logger := logrus.New()

	// Log DEBUG and higher level logs to STDERR
	logger.Level = logrus.DebugLevel
	logger.Out = os.Stderr

	// Send only ERROR and higher level logs to Sentry
	sentryLevels := []logrus.Level{logrus.ErrorLevel, logrus.FatalLevel, logrus.PanicLevel}

	sentryHook, err := sentrylogrus.New(sentryLevels, sentry.ClientOptions{
		Dsn: "",
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
		Debug:            true,
		AttachStacktrace: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentryHook.Flush(5 * time.Second)
	logger.AddHook(sentryHook)

	// The following line is logged to STDERR, but not to Sentry
	logger.Infof("Application has started")

	// The following line is logged to STDERR and also sent to Sentry
	logger.Errorf("oh no!")
}
