package utils_test

import (
	"github.com/getsentry/sentry-go/otel/internal/utils"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	"net/http"
	"testing"
)

func TestParseSpanAttributes(t *testing.T) {
	t.Run("Should prefer httpRoute over httpTarget", func(t *testing.T) {
		span := &mockReadOnlySpan{
			status: trace.Status{
				Code: codes.Ok,
			},
			attributes: []attribute.KeyValue{
				semconv.HTTPMethodKey.String(http.MethodOptions),
				semconv.HTTPTargetKey.String("/projects/123/settings?q=proj#123"),
				semconv.HTTPRouteKey.String("/projects/:projectID/settings"),
				semconv.HTTPURLKey.String("https://sentry.io/projects/:projectID/settings?q=proj#123"),
			},
		}

		parsedAttributes := utils.ParseSpanAttributes(span)
		assert.Equal(t, "OPTIONS /projects/:projectID/settings", parsedAttributes.Description)
	})
}
