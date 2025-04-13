package main

import (
	"context"

	sentrytracing "github.com/getsentry/sentry-go/tracing"
)

func main() {
	ctx, span := sentrytracing.StartSpan(context.Background(), "my_span")
	span.SetAttributes(sentrytracing.String("my_attribute", "my_value"), sentrytracing.Boolean("my_bool", true))
	defer span.Finish()

	_, span = sentrytracing.StartSpan(ctx, "my_span_2")
	defer span.Finish()
}
