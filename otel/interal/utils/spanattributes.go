package utils

import (
	"fmt"
	"strings"

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
			return descriptionForHttpMethod(s)
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
		// TODO(michi) Do we have a better default?
		Op:          "undefined",
		Description: s.Name(),
		Source:      sentry.SourceCustom,
	}
}

func descriptionForDbSystem(s otelSdkTrace.ReadOnlySpan) SpanAttributes {
	description := s.Name()
	// Use DB statement (Ex "SELECT * FROM table") if possible as description.
	for _, attribute := range s.Attributes() {
		if attribute.Key == semconv.DBStatementKey {
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

func descriptionForHttpMethod(s otelSdkTrace.ReadOnlySpan) SpanAttributes {
	opParts := []string{"http"}

	switch s.SpanKind() {
	case otelTrace.SpanKindClient:
		opParts = append(opParts, "client")
	case otelTrace.SpanKindServer:
		opParts = append(opParts, "server")
	}

	var httpTarget string
	var httpRoute string
	var httpMethod string

	for _, attribute := range s.Attributes() {
		if attribute.Key == semconv.HTTPTargetKey {
			httpTarget = attribute.Value.AsString()
			break
		}
		if attribute.Key == semconv.HTTPRouteKey {
			httpRoute = attribute.Value.AsString()
			break
		}
		if attribute.Key == semconv.HTTPMethodKey {
			httpMethod = attribute.Value.AsString()
			break
		}
	}

	var httpPath string
	if httpTarget != "" {
		httpPath = httpTarget
	} else if httpRoute != "" {
		httpPath = httpRoute
	}

	if httpPath == "" {
		return SpanAttributes{
			Op:          strings.Join(opParts[:], "."),
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
		Op:          strings.Join(opParts[:], "."),
		Description: description,
		Source:      source,
	}
}
