package utils

import (
	"context"
	"strings"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel/sdk/trace"
)

func IsSentryRequestSpan(ctx context.Context, s trace.ReadOnlySpan) bool {
	return isSentryRequestURL(ctx, sentryRequestURL(s.Attributes()))
}

func isSentryRequestURL(ctx context.Context, url string) bool {
	if url == "" {
		return false
	}

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
