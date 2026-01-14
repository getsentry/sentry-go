package main

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:           "https://3c3fd18b3fd44566aeab11385f391a48@o447951.ingest.us.sentry.io/5774600",
		EnableMetrics: true,
		Debug:         true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	meter := sentry.NewMeter(ctx)
	// Attaching permanent attributes on the meter
	meter.SetAttributes(
		attribute.String("version", "1.0.0"),
	)

	// Count metrics to measure occurrences of an event.
	meter.Count("sent_emails", 1, sentry.MeterOptions{
		Attributes: []attribute.Builder{
			attribute.String("email.provider", "sendgrid"),
			attribute.Int("email.number_of_recipients", 3),
		},
	})

	// Distribution metrics to measure the statistical distribution of a set of values.
	// Useful for measuring things and keeping track of the patterns, e.g. file sizes, response times, etc.
	meter.Distribution("file_upload_size", 3.14, sentry.MeterOptions{
		Unit: "MB", // Unit is optional, but it's recommended!
		Attributes: []attribute.Builder{
			attribute.String("file.type", "image/png"),
			attribute.String("bucket.region", "us-west-2"),
			attribute.String("bucket.name", "user-uploads"),
		},
	})

	// Gauge metrics to measure a value at a specific point in time.
	// Useful for measuring values that can go up and down, e.g. temperature, memory usage, etc.
	meter.Gauge("active_chat_conversations", 7, sentry.MeterOptions{
		Unit: "chat_rooms", // Unit is optional, but it's recommended!
		Attributes: []attribute.Builder{
			attribute.String("region", "asia-northeast1"),
		},
	})
}
