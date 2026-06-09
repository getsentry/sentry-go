package utils

import (
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.30.0"
)

var (
	legacyDBStatementKey    = attribute.Key("db.statement")
	legacyDBSystemKey       = attribute.Key("db.system")
	legacyHTTPMethodKey     = attribute.Key("http.method")
	legacyHTTPStatusCodeKey = attribute.Key("http.status_code")
	legacyHTTPTargetKey     = attribute.Key("http.target")
	legacyHTTPURLKey        = attribute.Key("http.url")
)

func isKey(key attribute.Key, candidates ...attribute.Key) bool {
	for _, candidate := range candidates {
		if key == candidate {
			return true
		}
	}

	return false
}

func spanAttributeString(attributes []attribute.KeyValue, keys ...attribute.Key) string {
	for _, attribute := range attributes {
		if isKey(attribute.Key, keys...) {
			return attribute.Value.AsString()
		}
	}

	return ""
}

func isHTTPMethodKey(key attribute.Key) bool {
	return isKey(key, legacyHTTPMethodKey, semconv.HTTPRequestMethodKey)
}

func isDBSystemKey(key attribute.Key) bool {
	return isKey(key, legacyDBSystemKey, semconv.DBSystemNameKey)
}

func isDBStatementKey(key attribute.Key) bool {
	return isKey(key, legacyDBStatementKey, semconv.DBQueryTextKey)
}

func isHTTPStatusCodeKey(key attribute.Key) bool {
	return isKey(key, legacyHTTPStatusCodeKey, semconv.HTTPResponseStatusCodeKey)
}

func sentryRequestURL(attributes []attribute.KeyValue) string {
	return spanAttributeString(attributes, legacyHTTPURLKey, semconv.URLFullKey)
}

func httpSpanData(attributes []attribute.KeyValue) (method, route, target, fullURL string) {
	return spanAttributeString(attributes, legacyHTTPMethodKey, semconv.HTTPRequestMethodKey),
		spanAttributeString(attributes, semconv.HTTPRouteKey),
		spanAttributeString(attributes, legacyHTTPTargetKey, semconv.URLPathKey),
		spanAttributeString(attributes, legacyHTTPURLKey, semconv.URLFullKey)
}
