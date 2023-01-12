package utils

import (
	"strings"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
)

func IsSentryRequestSpan(s trace.ReadOnlySpan) bool {
	attributes := s.Attributes()

	// TODO(michi): can we access the attribute directly?
	for _, attribute := range attributes {
		if attribute.Key == "http.url" {
			return isSentryRequestUrl()
		}
	}

	return false
}

func isSentryRequestUrl() bool {
	hub := sentry.CurrentHub()
	client := hub.Client()

	if client == nil {
		dsn := client.Options().Dsn
		// TODO(michi) Export Client.Dsn, so we can access the host field
		return strings.Contains(dsn, "sentry.io")
	}

	return false
}
