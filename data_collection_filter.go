package sentry

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// defaultSensitiveTerms is the canonical list of case-insensitive,
// partial-match terms used for scrubbing.
//
// See https://develop.sentry.dev/sdk/foundations/client/data-collection/#sensitive-denylist
var defaultSensitiveTerms = []string{
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

// filterHeaders flattens headers and applies a KeyValueCollectionBehavior to
// them. Keys are always preserved and values are replaced with "[Filtered]".
// Cookie and Set-Cookie values are filtered per cookie.
//
// Returns nil when the mode is CollectionOff.
func (dc DataCollection) filterHeaders(headers http.Header, behavior *KeyValueCollectionBehavior) map[string]string {
	if behavior != nil && behavior.Mode == CollectionOff {
		return nil
	}
	result := make(map[string]string, len(headers))
	for key, values := range headers {
		var value string
		switch {
		case len(values) == 0:
			continue
		case strings.EqualFold(key, "Cookie"):
			if value = dc.FilterCookies(values); value == "" {
				continue
			}
		case strings.EqualFold(key, "Set-Cookie"):
			if value = dc.FilterSetCookies(values); value == "" {
				continue
			}
		default:
			value = strings.Join(values, ",")
		}
		if dc.shouldFilterKey(key, behavior) {
			value = filteredValue
		}
		result[key] = value
	}
	return result
}

// FilterRequestHeaders applies the configured request-header and cookie
// collection behaviors to headers.
func (dc DataCollection) FilterRequestHeaders(headers http.Header) map[string]string {
	var behavior *KeyValueCollectionBehavior
	if dc.HTTPHeaders != nil {
		behavior = dc.HTTPHeaders.Request
	}
	return dc.filterHeaders(headers, behavior)
}

// FilterResponseHeaders applies the configured response-header and cookie
// collection behaviors to headers.
func (dc DataCollection) FilterResponseHeaders(headers http.Header) map[string]string {
	var behavior *KeyValueCollectionBehavior
	if dc.HTTPHeaders != nil {
		behavior = dc.HTTPHeaders.Response
	}
	return dc.filterHeaders(headers, behavior)
}

// CollectHTTPBody reports whether the given body type should be collected.
func (dc DataCollection) CollectHTTPBody(bt BodyType) bool {
	return dc.HTTPBodies == nil || slices.Contains(dc.HTTPBodies, bt)
}

// CollectCookies reports whether cookies should be collected.
func (dc DataCollection) CollectCookies() bool {
	return dc.Cookies == nil || dc.Cookies.Mode != CollectionOff
}

// CollectQueryParams reports whether query parameters should be collected.
func (dc DataCollection) CollectQueryParams() bool {
	return dc.URLQueryParams == nil || dc.URLQueryParams.Mode != CollectionOff
}

// FilterQueryString applies the configured query-parameter collection behavior.
func (dc DataCollection) FilterQueryString(rawQuery string) string {
	return dc.filterRawQuery(rawQuery, dc.URLQueryParams)
}

// FilterURL applies query-parameter filtering to u and returns its redacted string form.
func (dc DataCollection) FilterURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	filtered := *u
	filtered.User = nil
	filtered.RawQuery = dc.FilterQueryString(u.RawQuery)
	filtered.Fragment, filtered.RawFragment = "", ""
	return filtered.String()
}

// FilterCookies applies the configured cookie collection behavior.
func (dc DataCollection) FilterCookies(values []string) string {
	if !dc.CollectCookies() {
		return ""
	}
	parsed := parseKeyValueStrings(values, ';')
	parts := make([]string, 0, len(parsed))
	for key, value := range parsed {
		if dc.shouldFilterKey(key, dc.Cookies) {
			value = filteredValue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "; ")
}

// FilterSetCookies applies the configured cookie collection behavior to
// Set-Cookie header values.
func (dc DataCollection) FilterSetCookies(values []string) string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if cookie := dc.filterSetCookie(value); cookie != "" {
			filtered = append(filtered, cookie)
		}
	}
	return strings.Join(filtered, ", ")
}

func (dc DataCollection) filterSetCookie(setCookie string) string {
	if !dc.CollectCookies() {
		return ""
	}
	parts := strings.Split(setCookie, ";")
	name, value, ok := strings.Cut(parts[0], "=")
	name = strings.TrimSpace(name)
	if !ok || name == "" {
		return ""
	}
	if dc.shouldFilterKey(name, dc.Cookies) {
		value = filteredValue
	}
	parts[0] = name + "=" + value

	for i := 1; i < len(parts); i++ {
		attribute, _, ok := strings.Cut(parts[i], "=")
		if !ok {
			continue
		}
		if name := strings.TrimSpace(attribute); name != "" && dc.shouldFilterKey(name, dc.Cookies) {
			parts[i] = attribute + "=" + filteredValue
		}
	}

	return strings.Join(parts, ";")
}

// FilterHTTPBody applies sensitive-key filtering to parseable HTTP body data.
// Opaque raw bodies are replaced entirely.
func (dc DataCollection) FilterHTTPBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}
	contentType = strings.ToLower(contentType)

	if strings.Contains(contentType, "application/json") || looksLikeJSON(body) {
		if filtered, ok := dc.filterJSONBody(body); ok {
			return filtered
		}
	}

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		return dc.filterRawQuery(string(body), nil)
	}

	return filteredValue
}

func looksLikeJSON(body []byte) bool {
	body = bytes.TrimLeft(body, " \t\r\n")
	return len(body) > 0 && (body[0] == '{' || body[0] == '[')
}

// filterJSONBody filters a JSON body, keeping numbers as written.
func (dc DataCollection) filterJSONBody(body []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil || dec.More() {
		return "", false
	}

	var filtered strings.Builder
	enc := json.NewEncoder(&filtered)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(dc.filterJSON(value, false)); err != nil {
		return "", false
	}
	return strings.TrimSuffix(filtered.String(), "\n"), true
}

// filterJSON filters a decoded JSON value in place. Scalars that are not
// object values are filtered.
func (dc DataCollection) filterJSON(value any, keyed bool) any {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if dc.shouldFilterKey(key, nil) {
				value[key] = filteredValue
			} else {
				value[key] = dc.filterJSON(child, true)
			}
		}
	case []any:
		for i, child := range value {
			value[i] = dc.filterJSON(child, keyed)
		}
	default:
		if !keyed {
			return filteredValue
		}
	}
	return value
}

func (dc DataCollection) filterRawQuery(rawQuery string, behavior *KeyValueCollectionBehavior) string {
	if rawQuery == "" || (behavior != nil && behavior.Mode == CollectionOff) {
		return ""
	}
	pairs := strings.Split(rawQuery, "&")
	for i, pair := range pairs {
		if pair == "" {
			continue
		}
		rawKey, _, _ := strings.Cut(pair, "=")
		key, err := url.QueryUnescape(rawKey)
		if err != nil || strings.Contains(pair, ";") || dc.shouldFilterKey(key, behavior) {
			pairs[i] = rawKey + "=" + filteredValue
		}
	}
	return strings.Join(pairs, "&")
}

// filterURLErrors applies query-parameter filtering to the URL of each *url.Error.
func (dc DataCollection) filterURLErrors(exceptions []Exception) {
	for _, exception := range exceptions {
		if exception.Type != "*url.Error" {
			continue
		}
		op, rest, _ := strings.Cut(exception.Value, " ")
		quoted, err := strconv.QuotedPrefix(rest)
		if err != nil {
			continue
		}
		rawURL, _ := strconv.Unquote(quoted)
		filteredURL := filteredValue
		if u, err := url.Parse(rawURL); err == nil {
			filteredURL = dc.FilterURL(u)
		}
		old, filtered := op+" "+quoted, op+" "+strconv.Quote(filteredURL)
		for i := range exceptions {
			exceptions[i].Value = strings.ReplaceAll(exceptions[i].Value, old, filtered)
		}
	}
}

// shouldFilterKey reports whether a key's value should be redacted under the
// given behavior. It combines the built-in sensitive terms with the behavior's
// user-provided terms.
func (dc DataCollection) shouldFilterKey(key string, behavior *KeyValueCollectionBehavior) bool {
	key = strings.ToLower(key)
	if matchesAnyTerm(key, defaultSensitiveTerms) || matchesAnyTerm(key, dc.sensitiveTerms) {
		return true
	}
	if behavior == nil {
		return false
	}
	switch behavior.Mode {
	case CollectionOff:
		return true
	case CollectionAllowList:
		return !matchesAnyTerm(key, behavior.Terms)
	default:
		return matchesAnyTerm(key, behavior.Terms)
	}
}

// matchesAnyTerm reports whether the lowercase key contains any of the terms,
// ignoring their case.
func matchesAnyTerm(key string, terms []string) bool {
	return slices.ContainsFunc(terms, func(term string) bool {
		return strings.Contains(key, strings.ToLower(term))
	})
}

// parseKeyValueStrings splits strings like "a=1; b=2" into a map.
// Malformed parts without '=' and parts with empty keys are skipped.
func parseKeyValueStrings(values []string, separator rune) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		for _, part := range strings.Split(value, string(separator)) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			key, value, ok := strings.Cut(part, "=")
			if !ok || strings.TrimSpace(key) == "" {
				continue
			}
			result[key] = value
		}
	}
	return result
}
