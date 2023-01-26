//go:build go1.18

package utils

import (
	"strings"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
)

func IsSentryRequestSpan(s trace.ReadOnlySpan) bool {
	attributes := s.Attributes()

	// TODO(michi): can we access the attribute directly?
	for _, attribute := range attributes {
		if attribute.Key == semconv.HTTPURLKey {
			return isSentryRequestUrl(attribute.Value.AsString())
		}
	}

	return false
}

func isSentryRequestUrl(url string) bool {
	hub := sentry.CurrentHub()
	client := hub.Client()

	if client != nil {
		dsn, _ := sentry.NewDsn(client.Options().Dsn)

		return strings.Contains(url, dsn.GetHost())
	}

	return false
}
