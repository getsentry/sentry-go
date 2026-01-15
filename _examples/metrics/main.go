package main

import (
	"context"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:   "",
		Debug: true,
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	ctx := context.Background()
	meter := sentry.NewMeter()
	// Attaching permanent attributes on the meter
	meter.SetAttributes(
		attribute.String("version", "1.0.0"),
	)

	// Count metrics to measure occurrences of an event.
	// The context is used to link the metric to the active trace/span.
	meter.Count(ctx, "sent_emails", 1, sentry.MeterOptions{
		Attributes: []attribute.Builder{
			attribute.String("email.provider", "sendgrid"),
			attribute.Int("email.number_of_recipients", 3),
		},
	})

	// Distribution metrics to measure the statistical distribution of a set of values.
	// Useful for measuring things and keeping track of the patterns, e.g. file sizes, response times, etc.
	meter.Distribution(ctx, "file_upload_size", 3.14, sentry.MeterOptions{
		Unit: sentry.UnitMegabyte, // Using standard unit constants
		Attributes: []attribute.Builder{
			attribute.String("file.type", "image/png"),
			attribute.String("bucket.region", "us-west-2"),
			attribute.String("bucket.name", "user-uploads"),
		},
	})

	// Distribution metric with duration unit
	meter.Distribution(ctx, "response_time", 123.5, sentry.MeterOptions{
		Unit: sentry.UnitMillisecond,
		Attributes: []attribute.Builder{
			attribute.String("endpoint", "/api/users"),
			attribute.String("method", "GET"),
		},
	})

	// Gauge metrics to measure a value at a specific point in time.
	// Useful for measuring values that can go up and down, e.g. temperature, memory usage, etc.
	meter.Gauge(ctx, "memory_usage", 512.0, sentry.MeterOptions{
		Unit: sentry.UnitMebibyte, // Using binary unit (MiB)
		Attributes: []attribute.Builder{
			attribute.String("process", "worker"),
		},
	})

	// Gauge metric with percentage
	meter.Gauge(ctx, "cpu_usage", 0.75, sentry.MeterOptions{
		Unit: sentry.UnitRatio, // Value from 0.0 to 1.0
		Attributes: []attribute.Builder{
			attribute.String("core", "0"),
		},
	})

	// Example using a custom scope for isolating metrics
	// This is useful when you want to capture metrics with a specific scope
	// that has different context data (user, tags, etc.) than the current scope
	customScope := sentry.NewScope()
	customScope.SetUser(sentry.User{
		ID:    "user-123",
		Email: "user@example.com",
	})
	customScope.SetTag("environment", "staging")

	meter.Distribution(ctx, "api_latency", 250.0, sentry.MeterOptions{
		Unit: sentry.UnitMillisecond,
		Attributes: []attribute.Builder{
			attribute.String("endpoint", "/api/orders"),
		},
		Scope: customScope, // Use a custom scope for this metric
	})
}
