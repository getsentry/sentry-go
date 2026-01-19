package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	sentryhttp "github.com/getsentry/sentry-go/http"
)

var meter sentry.Meter

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn:              "",
		Debug:            true,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
		BeforeSendMetric: func(metric *sentry.Metric) *sentry.Metric {
			// Filter metrics based on metric type and value
			switch metric.Type {
			case sentry.MetricTypeCounter:
				if v, ok := metric.Value.Int64(); ok && v < 5 {
					return nil // drop low-value counters
				}
			case sentry.MetricTypeGauge:
				if v, ok := metric.Value.Float64(); ok && v < 10.0 {
					return nil // drop low gauge readings
				}
			case sentry.MetricTypeDistribution:
				// keep all distributions
			}

			// Alternative: handle value types directly
			if v, ok := metric.Value.Int64(); ok && v > 1 {
				// handle all int64 values (counters)
			}

			return metric
		},
	})
	if err != nil {
		panic(err)
	}
	defer sentry.Flush(2 * time.Second)

	meter = sentry.NewMeter(context.Background())
	// Attaching permanent attributes on the meter
	meter.SetAttributes(
		attribute.String("version", "1.0.0"),
	)

	// Count metrics to measure occurrences of an event.
	meter.Count("sent_emails", 1,
		sentry.WithAttributes(
			attribute.String("email.provider", "sendgrid"),
			attribute.Int("email.number_of_recipients", 3),
		),
	)

	// Distribution metrics to measure the statistical distribution of a set of values.
	// Useful for measuring things and keeping track of the patterns, e.g. file sizes, response times, etc.
	meter.Distribution("file_upload_size", 3.14,
		sentry.WithUnit(sentry.UnitMegabyte), // Using standard unit constants
		sentry.WithAttributes(
			attribute.String("file.type", "image/png"),
			attribute.String("bucket.region", "us-west-2"),
			attribute.String("bucket.name", "user-uploads"),
		),
	)

	// Distribution metric with duration unit
	meter.Distribution("response_time", 123.5,
		sentry.WithUnit(sentry.UnitMillisecond),
		sentry.WithAttributes(
			attribute.String("endpoint", "/api/users"),
			attribute.String("method", "GET"),
		),
	)

	// Gauge metrics to measure a value at a specific point in time.
	// Useful for measuring values that can go up and down, e.g. temperature, memory usage, etc.
	meter.Gauge("memory_usage", 512.0,
		sentry.WithUnit(sentry.UnitMebibyte), // Using binary unit (MiB)
		sentry.WithAttributes(
			attribute.String("process", "worker"),
		),
	)

	// Gauge metric with percentage
	meter.Gauge("cpu_usage", 0.75,
		sentry.WithUnit(sentry.UnitRatio), // Value from 0.0 to 1.0
		sentry.WithAttributes(
			attribute.String("core", "0"),
		),
	)

	// Example using a custom scope for isolating metrics
	// This is useful when you want to capture metrics with a specific scope
	// that has different context data (user, tags, etc.) than the current scope
	customScope := sentry.NewScope()
	customScope.SetUser(sentry.User{
		ID:    "user-123",
		Email: "user@example.com",
	})
	customScope.SetTag("environment", "staging")

	meter.Distribution("api_latency", 250.0,
		sentry.WithUnit(sentry.UnitMillisecond),
		sentry.WithScopeOverride(customScope), // Use a custom scope for this metric
		sentry.WithAttributes(
			attribute.String("endpoint", "/api/orders"),
		),
	)

	sentryHandler := sentryhttp.New(sentryhttp.Options{})
	http.HandleFunc("/", sentryHandler.HandleFunc(handler))

	fmt.Println("Listening on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

// handler is an example of using `WithCtx` to link the metric with the correct request trace.
func handler(w http.ResponseWriter, r *http.Request) {
	// Use r.Context() and `WithCtx` to link the metric to the current request's span.
	// The sentryhttp middleware adds the span to the request context.
	meter.WithCtx(r.Context()).Count("page_views", 1,
		sentry.WithAttributes(
			attribute.String("path", r.URL.Path),
			attribute.String("method", r.Method),
		),
	)

	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "Hello, World!")
}
