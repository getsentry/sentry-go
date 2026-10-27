package sentry

import (
	"net/http"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSensitiveDenyListTerms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		key  string
		want bool
	}{
		{"auth", true},
		{"token", true},
		{"secret", true},
		{"password", true},
		{"passwd", true},
		{"pwd", true},
		{"key", true},
		{"jwt", true},
		{"bearer", true},
		{"sso", true},
		{"saml", true},
		{"csrf", true},
		{"xsrf", true},
		{"credentials", true},
		{"session", true},
		{"sid", true},
		{"identity", true},
		{"Authorization", true},
		{"X-Auth-Token", true},
		{"X-API-Key", true},
		{"Content-Type", false},
		{"X-Request-Id", false},
	}

	dc := DataCollection{}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			if got := dc.shouldFilterKey(tt.key, nil); got != tt.want {
				t.Errorf("shouldFilterKey(%q, nil) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestNewClientDataCollectionHeaderFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		header         http.Header
		want           map[string]string
	}{
		{
			name: "default headers scrub built-in sensitive keys and join values",
			header: http.Header{
				"Authorization": {"Bearer abc123"},
				"Accept":        {"text/html", "application/json"},
				"X-Request-Id":  {"req-456"},
			},
			want: map[string]string{
				"Authorization": filteredValue,
				"Accept":        "text/html,application/json",
				"X-Request-Id":  "req-456",
			},
		},
		{
			name:   "cookies are parsed and filtered per cookie name",
			header: http.Header{"Cookie": {"debug; =bad; empty=", "user_session=secret; theme=dark"}},
			want:   map[string]string{"Cookie": "empty=; user_session=[Filtered]; theme=dark"},
		},
		{
			name:   "set-cookie attributes are preserved",
			header: http.Header{"Set-Cookie": {"sessionid=abc123; Path=/", "theme=dark; Secure"}},
			want:   map[string]string{"Set-Cookie": "sessionid=[Filtered]; Path=/, theme=dark; Secure"},
		},
		{
			name: "cookies off omits cookie headers",
			dataCollection: &DataCollection{
				Cookies: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			header: http.Header{"Cookie": {"theme=dark"}, "Set-Cookie": {"theme=dark"}, "Accept": {"*/*"}},
			want:   map[string]string{"Accept": "*/*"},
		},
		{
			name: "allow list filters headers that are not allowed",
			dataCollection: &DataCollection{
				HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"ACCEPT"}}},
			},
			header: http.Header{"Accept": {"*/*"}, "Cookie": {"theme=dark"}},
			want:   map[string]string{"Accept": "*/*", "Cookie": filteredValue},
		},
		{
			name: "off mode omits collection",
			dataCollection: &DataCollection{
				HTTPHeaders: &HeaderCollectionConfig{Request: &KeyValueCollectionBehavior{Mode: CollectionOff}},
			},
			header: http.Header{"Accept": {"*/*"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			if diff := cmp.Diff(tt.want, dc.FilterRequestHeaders(tt.header), testutils.EquateKeyValueStrings()); diff != "" {
				t.Errorf("filtered headers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewClientDataCollectionQueryFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		query          string
		want           string
	}{
		{
			name: "custom deny terms are case-insensitive",
			dataCollection: &DataCollection{
				URLQueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"REQUEST-ID"}},
			},
			query: "x-request-id=req-456&page=1",
			want:  "x-request-id=[Filtered]&page=1",
		},
		{
			name: "allow list is case-insensitive and built-in sensitive keys still scrub",
			dataCollection: &DataCollection{
				URLQueryParams: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"PAGE", "PASSWORD"}},
			},
			query: "debug=true&page=1&password=secret",
			want:  "debug=[Filtered]&page=1&password=[Filtered]",
		},
		{
			name:  "flag-style and encoded query keys are filtered per key",
			query: "debug&pa%73sword=secret&page=1",
			want:  "debug&pa%73sword=[Filtered]&page=1",
		},
		{
			name:  "encoded query delimiters in values cannot inject sensitive parameters",
			query: "safe=hello%26password%3Dhunter2&page=1",
			want:  "safe=hello%26password%3Dhunter2&page=1",
		},
		{
			name:  "query keeps the original order and encoding",
			query: "token=abc123&q=a%20b%26c&page=5",
			want:  "token=[Filtered]&q=a%20b%26c&page=5",
		},
		{
			name:  "malformed query keeps raw values and filters undecodable keys",
			query: "safe=ok&token=secret&bad=%ZZ&pa%ZZss=x&a=1;token=x&page=1",
			want:  "safe=ok&token=[Filtered]&bad=%ZZ&pa%ZZss=[Filtered]&a=[Filtered]&page=1",
		},
		{
			name: "off mode omits collection",
			dataCollection: &DataCollection{
				URLQueryParams: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			query: "page=1",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			assert.Equal(t, tt.want, dc.FilterQueryString(tt.query))
		})
	}
}

func TestNewClientDataCollectionHTTPBodyFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		body           string
		contentType    string
		want           string
	}{
		{
			name:        "JSON uses built-in sensitive denylist",
			body:        `{"internal_id":"123","name":"Jane","password":"secret"}`,
			contentType: "application/json",
			want:        `{"internal_id":"123","name":"Jane","password":"[Filtered]"}`,
		},
		{
			name: "JSON ignores query param allow list",
			dataCollection: &DataCollection{
				URLQueryParams: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"name", "password"}},
			},
			body:        `{"id":"123","name":"Jane","password":"secret"}`,
			contentType: "application/json",
			want:        `{"id":"123","name":"Jane","password":"[Filtered]"}`,
		},
		{
			name: "query params off does not omit parseable body data",
			dataCollection: &DataCollection{
				URLQueryParams: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			body:        `{"name":"Jane","password":"secret"}`,
			contentType: "application/json",
			want:        `{"name":"Jane","password":"[Filtered]"}`,
		},
		{
			name:        "JSON numbers and characters are kept as written",
			body:        `{"id":12345678901234567890,"ratio":1.50,"q":"<a&b>"}`,
			contentType: "application/json",
			want:        `{"id":12345678901234567890,"q":"<a&b>","ratio":1.50}`,
		},
		{
			name:        "top-level JSON string is filtered",
			body:        `"Bearer secret"`,
			contentType: "application/json",
			want:        `"[Filtered]"`,
		},
		{
			name:        "scalars in nested JSON arrays are filtered",
			body:        `["user@example.com",["secret"]]`,
			contentType: "application/json",
			want:        `["[Filtered]",["[Filtered]"]]`,
		},
		{
			name:        "JSON array is sniffed regardless of content type",
			body:        ` ["secret"]`,
			contentType: "text/plain",
			want:        `["[Filtered]"]`,
		},
		{
			name:        "multiple JSON values are filtered entirely",
			body:        "{\"a\":1}\n{\"b\":2}",
			contentType: "application/json",
			want:        filteredValue,
		},
		{
			name:        "form body uses built-in sensitive denylist",
			body:        "internal_id=123&name=Jane&password=secret",
			contentType: "application/x-www-form-urlencoded",
			want:        "internal_id=123&name=Jane&password=[Filtered]",
		},
		{
			name:        "encoded form delimiters in values cannot inject sensitive parameters",
			body:        "safe=hello%26password%3Dhunter2&page=1",
			contentType: "application/x-www-form-urlencoded",
			want:        "safe=hello%26password%3Dhunter2&page=1",
		},
		{
			name:        "malformed form values are kept",
			body:        "safe=ok&bad=%ZZ&token=secret",
			contentType: "application/x-www-form-urlencoded; charset=UTF-8",
			want:        "safe=ok&bad=%ZZ&token=[Filtered]",
		},
		{
			name:        "opaque body is filtered entirely",
			body:        "plain secret",
			contentType: "text/plain",
			want:        filteredValue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			assert.Equal(t, tt.want, dc.FilterHTTPBody([]byte(tt.body), tt.contentType))
		})
	}
}

func TestFilterSetCookies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		setCookies     []string
		want           string
	}{
		{
			name:       "sensitive cookie value redacted and attributes preserved",
			setCookies: []string{"sessionid=abc123; Path=/; HttpOnly; Secure; Max-Age=3600"},
			want:       "sessionid=[Filtered]; Path=/; HttpOnly; Secure; Max-Age=3600",
		},
		{
			name:       "non-sensitive cookie unchanged",
			setCookies: []string{"theme=dark; Path=/settings; SameSite=Lax"},
			want:       "theme=dark; Path=/settings; SameSite=Lax",
		},
		{
			name:       "attributes with commas and equals are not misparsed as cookies",
			setCookies: []string{"theme=dark; Expires=Wed, 21 Oct 2015 07:28:00 GMT; Domain=example.com"},
			want:       "theme=dark; Expires=Wed, 21 Oct 2015 07:28:00 GMT; Domain=example.com",
		},
		{
			name:       "sensitive attribute value redacted",
			setCookies: []string{"theme=dark; Password=hunter=2; Path=/; HttpOnly"},
			want:       "theme=dark; Password=[Filtered]; Path=/; HttpOnly",
		},
		{
			name:       "cookie without attributes",
			setCookies: []string{"token=abc"},
			want:       "token=[Filtered]",
		},
		{
			name: "multiple header values are filtered separately",
			setCookies: []string{
				"sessionid=abc123; Path=/; HttpOnly",
				"theme=dark; Expires=Wed, 21 Oct 2015 07:28:00 GMT",
			},
			want: "sessionid=[Filtered]; Path=/; HttpOnly, theme=dark; Expires=Wed, 21 Oct 2015 07:28:00 GMT",
		},
		{
			name: "custom deny terms apply to the cookie name",
			dataCollection: &DataCollection{
				Cookies: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"theme"}},
			},
			setCookies: []string{"theme=dark; Path=/"},
			want:       "theme=[Filtered]; Path=/",
		},
		{
			name: "custom deny terms apply to attributes",
			dataCollection: &DataCollection{
				Cookies: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"domain"}},
			},
			setCookies: []string{"theme=dark; Domain=private.example.com; Secure"},
			want:       "theme=dark; Domain=[Filtered]; Secure",
		},
		{
			name: "allow list still scrubs built-in sensitive names",
			dataCollection: &DataCollection{
				Cookies: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"sessionid", "theme"}},
			},
			setCookies: []string{"sessionid=abc123; Path=/"},
			want:       "sessionid=[Filtered]; Path=[Filtered]",
		},
		{
			name: "off mode omits collection",
			dataCollection: &DataCollection{
				Cookies: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			setCookies: []string{"theme=dark; Path=/"},
			want:       "",
		},
		{
			name:       "malformed value without name=value pair is dropped",
			setCookies: []string{"HttpOnly"},
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			assert.Equal(t, tt.want, dc.FilterSetCookies(tt.setCookies))
		})
	}
}

func TestNewClientDataCollectionCollectHTTPBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		want           []BodyType
	}{
		{name: "nil HTTPBodies collects all", dataCollection: &DataCollection{}, want: allBodyTypes()},
		{name: "empty HTTPBodies collects none", dataCollection: &DataCollection{HTTPBodies: []BodyType{}}, want: []BodyType{}},
		{name: "specific body types", dataCollection: &DataCollection{HTTPBodies: []BodyType{BodyIncomingRequest, BodyOutgoingRequest}}, want: []BodyType{BodyIncomingRequest, BodyOutgoingRequest}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			got := make([]BodyType, 0)
			for _, bt := range allBodyTypes() {
				if dc.CollectHTTPBody(bt) {
					got = append(got, bt)
				}
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtendedSensitiveTermsAreOptIn(t *testing.T) {
	t.Parallel()

	header := http.Header{
		"X-Forwarded-For": {"192.0.2.1"},
		"X-Real-Ip":       {"192.0.2.2"},
		"Remote-Addr":     {"192.0.2.3"},
		"X-User-Id":       {"user-123"},
	}

	explicit := newClientDataCollection(t, &DataCollection{})
	assert.Equal(t, map[string]string{
		"X-Forwarded-For": "192.0.2.1",
		"X-Real-Ip":       "192.0.2.2",
		"Remote-Addr":     "192.0.2.3",
		"X-User-Id":       "user-123",
	}, explicit.FilterRequestHeaders(header))

	legacy, err := NewClient(ClientOptions{Dsn: "https://key@sentry.io/1"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"X-Forwarded-For": filteredValue,
		"X-Real-Ip":       filteredValue,
		"Remote-Addr":     filteredValue,
		"X-User-Id":       filteredValue,
	}, legacy.GetDataCollection().FilterRequestHeaders(header))
}

func newClientDataCollection(t *testing.T, dc *DataCollection) DataCollection {
	t.Helper()

	if dc == nil {
		dc = &DataCollection{}
	}
	client, err := NewClient(ClientOptions{
		Dsn:            "https://key@sentry.io/1",
		DataCollection: dc,
	})
	if err != nil {
		t.Fatal(err)
	}
	return client.GetDataCollection()
}
