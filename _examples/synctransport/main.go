package main

import (
	"context"
	"log"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	const dsn = "https://hello@example.com/1337"
	sentrySyncTransport := sentry.NewHTTPSyncTransport(sentry.TransportOptions{
		Dsn: dsn, Timeout: 3 * time.Second,
	})

	_ = sentry.Init(sentry.ClientOptions{
		Dsn:       dsn,
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
