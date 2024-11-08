package utils

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
)

func IsSentryRequestSpan(ctx context.Context, s trace.ReadOnlySpan) bool {
	attributes := s.Attributes()

	// TODO(michi): can we access the attribute directly?
	for _, attribute := range attributes {
		if attribute.Key == semconv.HTTPURLKey {
			return isSentryRequestUrl(ctx, attribute.Value.AsString())
		}
	}

	return false
}

func isSentryRequestUrl(ctx context.Context, url string) bool {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
		if hub == nil {
			return false
		}
	}

	client := hub.Client()
	if client == nil {
		return false
	}

	dsn, err := sentry.NewDsn(client.Options().Dsn)
	if err != nil {
		return false
	}

	return strings.Contains(url, dsn.GetAPIURL().String())
}
