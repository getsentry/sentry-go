package utils

import (
	"fmt"
	"net/url"

	"github.com/getsentry/sentry-go"
	otelSdkTrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	otelTrace "go.opentelemetry.io/otel/trace"
)

type SpanAttributes struct {
	Op          string
	Description string
	Source      sentry.TransactionSource
}

func ParseSpanAttributes(s otelSdkTrace.ReadOnlySpan) SpanAttributes {
	for _, attribute := range s.Attributes() {
		if attribute.Key == semconv.HTTPMethodKey {
			return descriptionForHTTPMethod(s)
		}
		if attribute.Key == semconv.DBSystemKey {
			return descriptionForDbSystem(s)
		}
		if attribute.Key == semconv.RPCSystemKey {
			return SpanAttributes{
				Op:          "rpc",
				Description: s.Name(),
				Source:      sentry.SourceRoute,
			}
		}
		if attribute.Key == semconv.MessagingSystemKey {
			return SpanAttributes{
				Op:          "messaging",
				Description: s.Name(),
				Source:      sentry.SourceRoute,
			}
		}
		// TODO(michi) Check if this works for AWS Lambda and such.
		if attribute.Key == semconv.FaaSTriggerKey {
			return SpanAttributes{
				Op:          attribute.Value.AsString(),
				Description: s.Name(),
				Source:      sentry.SourceRoute,
			}
		}
	}

	return SpanAttributes{
		Op:          "", // becomes "default" in Relay
		Description: s.Name(),
		Source:      sentry.SourceCustom,
	}
}

func descriptionForDbSystem(s otelSdkTrace.ReadOnlySpan) SpanAttributes {
	description := s.Name()
	for _, attribute := range s.Attributes() {
		if attribute.Key == semconv.DBStatementKey {
			// TODO(michi)
			// Note: The value may be sanitized to exclude sensitive information.
			// See: https://pkg.go.dev/go.opentelemetry.io/otel/semconv/v1.12.0
			description = attribute.Value.AsString()
			break
		}
	}

	return SpanAttributes{
		Op:          "db",
		Description: description,
		Source:      sentry.SourceTask,
	}
}

func descriptionForHTTPMethod(s otelSdkTrace.ReadOnlySpan) SpanAttributes {
	op := "http"
	switch s.SpanKind() {
	case otelTrace.SpanKindClient:
		op = "http.client"
	case otelTrace.SpanKindServer:
		op = "http.server"
	}

	var httpTarget string
	var httpRoute string
	var httpMethod string
	var httpURL string

	for _, attribute := range s.Attributes() {
		switch attribute.Key {
		case semconv.HTTPTargetKey:
			httpTarget = attribute.Value.AsString()
		case semconv.HTTPRouteKey:
			httpRoute = attribute.Value.AsString()
		case semconv.HTTPMethodKey:
			httpMethod = attribute.Value.AsString()
		case semconv.HTTPURLKey:
			httpURL = attribute.Value.AsString()
		}
	}

	var httpPath string

	// Prefer httpRoute if available
	switch {
	case httpRoute != "":
		httpPath = httpRoute
	case httpTarget != "":
		if parsedURL, err := url.Parse(httpTarget); err == nil {
			// Do not include the query and fragment parts
			httpPath = parsedURL.Path
		} else {
			httpPath = httpTarget
		}
	case httpURL != "":
		// This is normally the HTTP-client case
		if parsedURL, err := url.Parse(httpURL); err == nil {
			// Do not include the query and fragment parts
			httpPath = fmt.Sprintf("%s://%s%s", parsedURL.Scheme, parsedURL.Host, parsedURL.Path)
		}
	}

	if httpPath == "" {
		return SpanAttributes{
			Op:          op,
			Description: s.Name(),
			Source:      sentry.SourceCustom,
		}
	}

	// Ex. description="GET /api/users".
	description := fmt.Sprintf("%s %s", httpMethod, httpPath)

	var source sentry.TransactionSource
	// If `httpPath` is a root path, then we can categorize the transaction source as route.
	if httpRoute != "" || httpPath == "/" {
		source = sentry.SourceRoute
	} else {
		source = sentry.SourceURL
	}

	return SpanAttributes{
		Op:          op,
		Description: description,
		Source:      source,
	}
}
