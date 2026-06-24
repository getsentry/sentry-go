package sentry

import (
	"encoding/json"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestIsSensitiveKey(t *testing.T) {
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

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()

			if got := isSensitiveKey(tt.key); got != tt.want {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestNewClientDataCollectionKeyValueFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		filter         func(*testing.T, DataCollection) map[string]string
		want           map[string]string
	}{
		{
			name: "default headers scrub built-in sensitive keys",
			filter: func(_ *testing.T, dc DataCollection) map[string]string {
				return dc.FilterRequestHeaders(map[string]string{
					"Authorization": "Bearer abc123",
					"Content-Type":  "application/json",
					"X-Request-Id":  "req-456",
				})
			},
			want: map[string]string{
				"Authorization": filteredValue,
				"Content-Type":  "application/json",
				"X-Request-Id":  "req-456",
			},
		},
		{
			name: "custom deny terms are case-insensitive",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"REQUEST-ID"}},
			},
			filter: func(t *testing.T, dc DataCollection) map[string]string {
				t.Helper()
				got, err := parseQueryString(dc.FilterQueryString("x-request-id=req-456&page=1"))
				if err != nil {
					t.Fatal(err)
				}
				return got
			},
			want: map[string]string{
				"x-request-id": filteredValue,
				"page":         "1",
			},
		},
		{
			name: "allow list is case-insensitive and built-in sensitive keys still scrub",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"PAGE", "PASSWORD"}},
			},
			filter: func(t *testing.T, dc DataCollection) map[string]string {
				t.Helper()
				got, err := parseQueryString(dc.FilterQueryString("debug=true&page=1&password=secret"))
				if err != nil {
					t.Fatal(err)
				}
				return got
			},
			want: map[string]string{
				"debug":    filteredValue,
				"page":     "1",
				"password": filteredValue,
			},
		},
		{
			name: "flag-style and encoded query keys are filtered per key",
			filter: func(t *testing.T, dc DataCollection) map[string]string {
				t.Helper()
				got, err := parseQueryString(dc.FilterQueryString("debug&pa%73sword=secret&page=1"))
				if err != nil {
					t.Fatal(err)
				}
				return got
			},
			want: map[string]string{
				"debug":    "",
				"password": filteredValue,
				"page":     "1",
			},
		},
		{
			name: "cookies are parsed and filtered per cookie name",
			filter: func(_ *testing.T, dc DataCollection) map[string]string {
				return parseKeyValueString(dc.FilterCookies("debug; user_session=secret; theme=dark"), ';')
			},
			want: map[string]string{
				"debug":        "",
				"user_session": filteredValue,
				"theme":        "dark",
			},
		},
		{
			name: "off mode omits collection",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			filter: func(_ *testing.T, dc DataCollection) map[string]string {
				return map[string]string{"query": dc.FilterQueryString("page=1")}
			},
			want: map[string]string{"query": ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			got := tt.filter(t, dc)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("filtered values mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewClientDataCollectionHTTPBodyFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		dataCollection *DataCollection
		body           []byte
		contentType    string
		want           map[string]any
	}{
		{
			name: "JSON uses custom deny terms",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"internal"}},
			},
			body:        []byte(`{"internal_id":"123","name":"Jane","password":"secret"}`),
			contentType: "application/json",
			want: map[string]any{
				"internal_id": filteredValue,
				"name":        "Jane",
				"password":    filteredValue,
			},
		},
		{
			name: "JSON uses allow list but sensitive keys still scrub",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionAllowList, Terms: []string{"name", "password"}},
			},
			body:        []byte(`{"id":"123","name":"Jane","password":"secret"}`),
			contentType: "application/json",
			want: map[string]any{
				"id":       filteredValue,
				"name":     "Jane",
				"password": filteredValue,
			},
		},
		{
			name: "form body uses custom deny terms",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: []string{"internal"}},
			},
			body:        []byte("internal_id=123&name=Jane&password=secret"),
			contentType: "application/x-www-form-urlencoded",
			want: map[string]any{
				"internal_id": filteredValue,
				"name":        "Jane",
				"password":    filteredValue,
			},
		},
		{
			name: "off mode omits parseable body data",
			dataCollection: &DataCollection{
				QueryParams: &KeyValueCollectionBehavior{Mode: CollectionOff},
			},
			body:        []byte(`{"name":"Jane"}`),
			contentType: "application/json",
			want:        map[string]any{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := newClientDataCollection(t, tt.dataCollection)
			got := dc.FilterHTTPBody(tt.body, tt.contentType)
			parsed := parseFilteredBody(t, got, tt.contentType)
			if diff := cmp.Diff(tt.want, parsed); diff != "" {
				t.Errorf("filtered body mismatch (-want +got):\n%s", diff)
			}
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
				if (&dc).CollectHTTPBody(bt) {
					got = append(got, bt)
				}
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("collected body types mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestPrivacyDenyTermsAreOptIn(t *testing.T) {
	t.Parallel()

	data := map[string]string{
		"x-forwarded-for": "192.0.2.1",
		"x-real-ip":       "192.0.2.2",
		"remote-addr":     "192.0.2.3",
		"x-user-id":       "user-123",
	}

	defaultFiltered := newClientDataCollection(t, &DataCollection{}).FilterRequestHeaders(data)
	for key, value := range data {
		if defaultFiltered[key] != value {
			t.Errorf("%s should not be filtered by default, got %q", key, defaultFiltered[key])
		}
	}

	strictFiltered := newClientDataCollection(t, &DataCollection{
		HTTPHeaders: &HeaderCollectionConfig{
			Request: &KeyValueCollectionBehavior{Mode: CollectionDenyList, Terms: PrivacyDenyTerms()},
		},
	}).FilterRequestHeaders(data)
	for key := range data {
		if strictFiltered[key] != filteredValue {
			t.Errorf("%s should be filtered with privacy deny terms, got %q", key, strictFiltered[key])
		}
	}
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

func parseFilteredBody(t *testing.T, body, contentType string) map[string]any {
	t.Helper()

	if body == "" {
		return map[string]any{}
	}
	if contentType == "application/json" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	form, err := parseQueryString(body)
	if err != nil {
		t.Fatal(err)
	}
	parsed := make(map[string]any, len(form))
	for key, value := range form {
		parsed[key] = value
	}
	return parsed
}
