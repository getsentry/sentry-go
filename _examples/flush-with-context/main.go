package main

import (
	"context"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:   "https://hello@example.com/1337",
		Debug: true,
	})

	sentry.CaptureMessage(context.Background(), "Event #1")
	sentry.CaptureMessage(context.Background(), "Event #2")
	sentry.CaptureMessage(context.Background(), "Event #3")

	go func() {
		sentry.CaptureMessage(context.Background(), "Event #4")
		sentry.CaptureMessage(context.Background(), "Event #5")
	}()

	fmt.Println("=> Flushing transport buffer")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	if sentry.FlushWithContext(ctx) {
		fmt.Println("=> All queued events delivered!")
	} else {
		fmt.Println("=> Flush timeout reached")
	}
}
