package utils

import (
	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
)

type SpanAttributes struct {
	Op          string
	Description string
	Source      sentry.TransactionSource
}

func ParseSpanAttributes(s trace.ReadOnlySpan) SpanAttributes {
	otelAttributes := s.Attributes()
	sentryAttributes := SpanAttributes{}

	for _, attribute := range otelAttributes {
		if attribute.Key == "http.method" {
			sentryAttributes.Op = "http"
			sentryAttributes.Source = sentry.SourceCustom
		}
		if attribute.Key == "db.system" {
			sentryAttributes.Op = "db"
			sentryAttributes.Source = sentry.SourceTask
		}
	}

	sentryAttributes.Description = s.Name()

	return sentryAttributes
}
