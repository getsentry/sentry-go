package utils_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.12.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/otel/internal/utils"
)

func TestParseSpanAttributes(t *testing.T) {
	t.Run("Handles HTTP spans", func(t *testing.T) {
		t.Run("Prefers httpRoute over httpTarget", func(t *testing.T) {
			span := &mockReadOnlySpan{
				name:     "<overridden>",
				spanKind: oteltrace.SpanKindServer,
				attributes: []attribute.KeyValue{
					semconv.HTTPMethodKey.String(http.MethodOptions),
					semconv.HTTPTargetKey.String("/projects/123/settings?q=proj#123"),
					semconv.HTTPRouteKey.String("/projects/:projectID/settings"),
					semconv.HTTPURLKey.String("https://sentry.io/projects/:projectID/settings?q=proj#123"),
				},
			}

			parsed := utils.ParseSpanAttributes(span)
			assert.Equal(t, "OPTIONS /projects/:projectID/settings", parsed.Description)
			assert.Equal(t, "http.server", parsed.Op)
			assert.Equal(t, sentry.SourceRoute, parsed.Source)
		})

		t.Run("Falls back to httpTarget when httpRoute is missing", func(t *testing.T) {
			span := &mockReadOnlySpan{
				name:     "<overridden>",
				spanKind: oteltrace.SpanKindClient,
				attributes: []attribute.KeyValue{
					semconv.HTTPMethodKey.String(http.MethodGet),
					semconv.HTTPTargetKey.String("/users?page=2"),
					semconv.HTTPURLKey.String("https://sentry.io/users?page=2"),
				},
			}

			parsed := utils.ParseSpanAttributes(span)
			assert.Equal(t, "GET /users", parsed.Description)
			assert.Equal(t, "http.client", parsed.Op)
			assert.Equal(t, sentry.SourceURL, parsed.Source)
		})

		t.Run("Falls back to httpUrl if route and target are missing", func(t *testing.T) {
			span := &mockReadOnlySpan{
				name:     "<overridden>",
				spanKind: oteltrace.SpanKindClient,
				attributes: []attribute.KeyValue{
					semconv.HTTPMethodKey.String(http.MethodGet),
					semconv.HTTPURLKey.String("https://sentry.io/api/v1/issues?limit=10"),
				},
			}

			parsed := utils.ParseSpanAttributes(span)
			assert.Equal(t, "GET https://sentry.io/api/v1/issues", parsed.Description)
			assert.Equal(t, "http.client", parsed.Op)
			assert.Equal(t, sentry.SourceURL, parsed.Source)
		})

		t.Run("Uses fallback when no URL info exists", func(t *testing.T) {
			span := &mockReadOnlySpan{
				name:     "Some description",
				spanKind: oteltrace.SpanKindClient,
				attributes: []attribute.KeyValue{
					semconv.HTTPMethodKey.String(http.MethodGet),
				},
			}

			parsed := utils.ParseSpanAttributes(span)
			assert.Equal(t, "Some description", parsed.Description)
			assert.Equal(t, "http.client", parsed.Op)
			assert.Equal(t, sentry.SourceCustom, parsed.Source)
		})
	})

	t.Run("Falls back to raw httpTarget when URL parsing fails", func(t *testing.T) {
		invalidTarget := "://bad:url::not_valid"
		span := &mockReadOnlySpan{
			name:     "<overridden>",
			spanKind: oteltrace.SpanKindServer,
			attributes: []attribute.KeyValue{
				semconv.HTTPMethodKey.String(http.MethodGet),
				semconv.HTTPTargetKey.String(invalidTarget),
			},
		}

		parsed := utils.ParseSpanAttributes(span)
		assert.Equal(t, fmt.Sprintf("GET %s", invalidTarget), parsed.Description)
		assert.Equal(t, "http.server", parsed.Op)
		assert.Equal(t, sentry.SourceURL, parsed.Source)
	})

	t.Run("Handles DB spans", func(t *testing.T) {
		t.Run("Includes DB statement in description", func(t *testing.T) {
			stmt := "SELECT * FROM users"
			span := &mockReadOnlySpan{
				name: "<overridden>",
				attributes: []attribute.KeyValue{
					semconv.DBSystemKey.String("postgresql"),
					semconv.DBStatementKey.String(stmt),
				},
			}

			parsed := utils.ParseSpanAttributes(span)
			assert.Equal(t, stmt, parsed.Description)
			assert.Equal(t, "db", parsed.Op)
			assert.Equal(t, sentry.SourceTask, parsed.Source)
		})
	})

	t.Run("Handles RPC spans", func(t *testing.T) {
		span := &mockReadOnlySpan{
			name: "rpc call",
			attributes: []attribute.KeyValue{
				semconv.RPCSystemKey.String("grpc"),
			},
		}

		parsed := utils.ParseSpanAttributes(span)
		assert.Equal(t, "rpc", parsed.Op)
		assert.Equal(t, "rpc call", parsed.Description)
		assert.Equal(t, sentry.SourceRoute, parsed.Source)
	})

	t.Run("Handles Messaging spans", func(t *testing.T) {
		span := &mockReadOnlySpan{
			name: "publish event",
			attributes: []attribute.KeyValue{
				semconv.MessagingSystemKey.String("kafka"),
			},
		}

		parsed := utils.ParseSpanAttributes(span)
		assert.Equal(t, "messaging", parsed.Op)
		assert.Equal(t, "publish event", parsed.Description)
		assert.Equal(t, sentry.SourceRoute, parsed.Source)
	})

	t.Run("Handles FaaS spans", func(t *testing.T) {
		span := &mockReadOnlySpan{
			name: "lambda triggered",
			attributes: []attribute.KeyValue{
				semconv.FaaSTriggerKey.String("http"),
			},
		}

		parsed := utils.ParseSpanAttributes(span)
		assert.Equal(t, "http", parsed.Op)
		assert.Equal(t, "lambda triggered", parsed.Description)
		assert.Equal(t, sentry.SourceRoute, parsed.Source)
	})

	t.Run("Handles unknown span types", func(t *testing.T) {
		span := &mockReadOnlySpan{
			name:       "some span",
			attributes: []attribute.KeyValue{},
		}

		parsed := utils.ParseSpanAttributes(span)
		assert.Equal(t, "", parsed.Op)
		assert.Equal(t, "some span", parsed.Description)
		assert.Equal(t, sentry.SourceCustom, parsed.Source)
	})
}
