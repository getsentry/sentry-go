package main

import (
	"context"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	sentrySyncTransport := sentry.NewHTTPSyncTransport()
	sentrySyncTransport.Timeout = time.Second * 3

	_ = sentry.Init(sentry.ClientOptions{
		Dsn:       "https://hello@example.com/1337",
		Debug:     true,
		Transport: sentrySyncTransport,
	})

	go func() {
		sentry.CaptureMessage(context.Background(), "Event #1")
		log.Println(1)
		sentry.CaptureMessage(context.Background(), "Event #2")
		log.Println(2)
	}()

	sentry.CaptureMessage(context.Background(), "Event #3")
	log.Println(3)
	sentry.CaptureMessage(context.Background(), "Event #4")
	log.Println(4)
	sentry.CaptureMessage(context.Background(), "Event #5")
	log.Println(5)

	go func() {
		sentry.CaptureMessage(context.Background(), "Event #6")
		log.Println(6)
		sentry.CaptureMessage(context.Background(), "Event #7")
		log.Println(7)
	}()
}
