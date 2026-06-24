package sentry

import (
	"encoding/json"
	"net/url"
	"strings"
)

// sensitiveDenyList is the canonical list of case-insensitive, partial-match terms used for scrubbing.
//
// See https://develop.sentry.dev/sdk/foundations/client/data-collection/#sensitive-denylist
var sensitiveDenyList = []string{
	"auth",
	"bearer",
	"credentials",
	"csrf",
	"identity",
	"jwt",
	"key",
	"passwd",
	"password",
	"pwd",
	"saml",
	"secret",
	"session",
	"sid",
	"sso",
	"token",
	"xsrf",
}

// filteredValue is the replacement for sensitive values.
const filteredValue = "[Filtered]"

// isSensitiveKey reports whether a key name matches the built-in sensitive denylist.
func isSensitiveKey(key string) bool {
	return matchesDenyTerms(key, sensitiveDenyList)
}

// filterKeyValues applies a KeyValueCollectionBehavior to a map of key-value
// pairs. Keys are always preserved and values are replaced with "[Filtered]".
//
// Returns nil when the mode is CollectionOff.
func filterKeyValues(data map[string]string, behavior *KeyValueCollectionBehavior) map[string]string {
	if behavior == nil {
		behavior = &KeyValueCollectionBehavior{}
	}

	switch behavior.Mode {
	case CollectionOff:
		return nil

	case CollectionAllowList:
		result := make(map[string]string, len(data))
		for k, v := range data {
			if isSensitiveKey(k) || !matchesDenyTerms(k, behavior.Terms) {
				result[k] = filteredValue
			} else {
				result[k] = v
			}
		}
		return result

	default: // CollectionDenyList (also handles zero value)
		result := make(map[string]string, len(data))
		for k, v := range data {
			if isSensitiveKey(k) || matchesDenyTerms(k, behavior.Terms) {
				result[k] = filteredValue
			} else {
				result[k] = v
			}
		}
		return result
	}
}

// FilterRequestHeaders applies the configured request-header collection behavior.
func (dc DataCollection) FilterRequestHeaders(headers map[string]string) map[string]string {
	var behavior *KeyValueCollectionBehavior
	if dc.HTTPHeaders != nil {
		behavior = dc.HTTPHeaders.Request
	}
	return filterKeyValues(headers, behavior)
}

// FilterResponseHeaders applies the configured response-header collection behavior.
func (dc DataCollection) FilterResponseHeaders(headers map[string]string) map[string]string {
	var behavior *KeyValueCollectionBehavior
	if dc.HTTPHeaders != nil {
		behavior = dc.HTTPHeaders.Response
	}
	return filterKeyValues(headers, behavior)
}

// CollectHTTPBody reports whether the given body type should be collected.
func (dc *DataCollection) CollectHTTPBody(bt BodyType) bool {
	if dc == nil || dc.HTTPBodies == nil {
		return true
	}
	for _, t := range dc.HTTPBodies {
		if t == bt {
			return true
		}
	}
	return false
}

// CollectCookies reports whether cookies should be collected.
func (dc *DataCollection) CollectCookies() bool {
	return dc == nil || dc.Cookies == nil || dc.Cookies.Mode != CollectionOff
}

// FilterQueryString applies the configured query-parameter collection behavior.
func (dc DataCollection) FilterQueryString(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parsed, err := parseQueryString(rawQuery)
	if err != nil {
		return filteredValue
	}
	filtered := filterKeyValues(parsed, dc.QueryParams)
	if filtered == nil {
		return ""
	}
	return joinKeyValuePairs(filtered, "&", "=")
}

// FilterCookies applies the configured cookie collection behavior.
func (dc DataCollection) FilterCookies(rawCookies string) string {
	if rawCookies == "" {
		return ""
	}
	parsed := parseKeyValueString(rawCookies, ';')
	if parsed == nil {
		return filteredValue
	}
	filtered := filterKeyValues(parsed, dc.Cookies)
	if filtered == nil {
		return ""
	}
	return joinKeyValuePairs(filtered, "; ", "=")
}

// FilterHTTPBody applies sensitive-key filtering to parseable HTTP body data.
// Opaque raw bodies are replaced entirely.
func (dc DataCollection) FilterHTTPBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}

	if strings.Contains(strings.ToLower(contentType), "application/json") || looksLikeJSON(body) {
		var value any
		if err := json.Unmarshal(body, &value); err == nil {
			filteredValue := filterJSONValue(value, dc.QueryParams)
			if filteredValue == nil {
				return ""
			}
			filtered, err := json.Marshal(filteredValue)
			if err == nil {
				return string(filtered)
			}
		}
	}

	if strings.Contains(strings.ToLower(contentType), "application/x-www-form-urlencoded") {
		if values, err := url.ParseQuery(string(body)); err == nil {
			filtered := filterKeyValues(flattenValues(values), dc.QueryParams)
			if filtered == nil {
				return ""
			}
			return joinKeyValuePairs(filtered, "&", "=")
		}
	}

	return filteredValue
}

func looksLikeJSON(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

func filterJSONValue(value any, behavior *KeyValueCollectionBehavior) any {
	if behavior != nil && behavior.Mode == CollectionOff {
		return nil
	}

	switch value := value.(type) {
	case map[string]any:
		filtered := make(map[string]any, len(value))
		for key, child := range value {
			if shouldFilterKey(key, behavior) {
				filtered[key] = filteredValue
			} else {
				filtered[key] = filterJSONValue(child, behavior)
			}
		}
		return filtered
	case []any:
		filtered := make([]any, len(value))
		for i, child := range value {
			filtered[i] = filterJSONValue(child, behavior)
		}
		return filtered
	default:
		return value
	}
}

func shouldFilterKey(key string, behavior *KeyValueCollectionBehavior) bool {
	if behavior == nil {
		behavior = &KeyValueCollectionBehavior{}
	}

	switch behavior.Mode {
	case CollectionOff:
		return true
	case CollectionAllowList:
		return isSensitiveKey(key) || !matchesDenyTerms(key, behavior.Terms)
	default:
		return isSensitiveKey(key) || matchesDenyTerms(key, behavior.Terms)
	}
}

// matchesDenyTerms reports whether the key (case-insensitive) contains any of
// the given terms as a substring.
func matchesDenyTerms(key string, terms []string) bool {
	lower := strings.ToLower(key)
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			return true
		}
	}
	return false
}

func parseQueryString(s string) (map[string]string, error) {
	values, err := url.ParseQuery(s)
	if err != nil {
		return nil, err
	}
	return flattenValues(values), nil
}

// parseKeyValueString splits a string like "a=1&b=2" or "a=1; b=2" into a
// map. Parts without '=' are treated as keys with empty values.
func parseKeyValueString(s string, separator rune) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(s, string(separator)) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			key = part
		}
		result[key] = value
	}
	return result
}

func flattenValues(values url.Values) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = strings.Join(value, ",")
	}
	return result
}

// joinKeyValuePairs reassembles a map into a string.
func joinKeyValuePairs(values map[string]string, separator, assignment string) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for k, v := range values {
		parts = append(parts, k+assignment+v)
	}
	return strings.Join(parts, separator)
}
