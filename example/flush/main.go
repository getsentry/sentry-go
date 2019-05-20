package main

import (
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	sentry.Init(sentry.ClientOptions{
		Dsn:   "https://definitelyincorrect@oiasaskjd.io/42",
		Debug: true,
	})

	sentry.CaptureMessage("Event #1")
	sentry.CaptureMessage("Event #2")
	sentry.CaptureMessage("Event #3")

	go func() {
		sentry.CaptureMessage("Event #4")
		sentry.CaptureMessage("Event #5")
	}()

	fmt.Println("=> Flushing transport buffer")

	if sentry.Flush(time.Second * 2) {
		fmt.Println("=> All queued events delivered!")
	} else {
		fmt.Println("=> Flush timeout reached")
	}
}
