package protocol

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type DsnTest struct {
	in     string
	dsn    *Dsn   // expected value after parsing
	url    string // expected Store API URL
	envURL string // expected Envelope API URL
}

var dsnTests = map[string]DsnTest{
	"AllFields": { //nolint:gosec // G101: not real credentials
		in: "https://public:secret@domain:8888/foo/bar/42",
		dsn: &Dsn{
			scheme:    SchemeHTTPS,
			publicKey: "public",
			secretKey: "secret",
			host:      "domain",
			port:      8888,
			path:      "/foo/bar",
			projectID: "42",
		},
		url:    "https://domain:8888/foo/bar/api/42/store/",
		envURL: "https://domain:8888/foo/bar/api/42/envelope/",
	},
	"MinimalSecure": {
		in: "https://public@domain/42",
		dsn: &Dsn{
			scheme:    SchemeHTTPS,
			publicKey: "public",
			host:      "domain",
			port:      443,
			projectID: "42",
		},
		url:    "https://domain/api/42/store/",
		envURL: "https://domain/api/42/envelope/",
	},
	"MinimalInsecure": {
		in: "http://public@domain/42",
		dsn: &Dsn{
			scheme:    SchemeHTTP,
			publicKey: "public",
			host:      "domain",
			port:      80,
			projectID: "42",
		},
		url:    "http://domain/api/42/store/",
		envURL: "http://domain/api/42/envelope/",
	},
	"IPv6WithPort": {
		in: "https://public@[2001:db8::1]:8888/42",
		dsn: &Dsn{
			scheme:    SchemeHTTPS,
			publicKey: "public",
			host:      "2001:db8::1",
			port:      8888,
			projectID: "42",
		},
		url:    "https://[2001:db8::1]:8888/api/42/store/",
		envURL: "https://[2001:db8::1]:8888/api/42/envelope/",
	},
	"IPv6DefaultPort": {
		in: "https://public@[::1]/42",
		dsn: &Dsn{
			scheme:    SchemeHTTPS,
			publicKey: "public",
			host:      "::1",
			port:      443,
			projectID: "42",
		},
		url:    "https://[::1]/api/42/store/",
		envURL: "https://[::1]/api/42/envelope/",
	},
}

func TestNewDsn(t *testing.T) {
	for name, tt := range dsnTests {
		t.Run(name, func(t *testing.T) {
			dsn, err := NewDsn(tt.in)
			if err != nil {
				t.Fatalf("NewDsn() error: %q", err)
			}
			// Internal fields
			if diff := cmp.Diff(tt.dsn, dsn, cmp.AllowUnexported(Dsn{})); diff != "" {
				t.Errorf("NewDsn() mismatch (-want +got):\n%s", diff)
			}
			url := dsn.GetAPIURL().String()
			if diff := cmp.Diff(tt.envURL, url); diff != "" {
				t.Errorf("dsn.EnvelopeAPIURL() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetAPIURLIsNeverNil(t *testing.T) {
	tests := map[string]string{
		"IPv6ZoneID":     "http://public@[fe80::1%25eth0]:9000/42",
		"EncodedNewline": "http://public@domain/42%0A",
		"EncodedNull":    "http://public@domain/42%00",
		"EncodedHash":    "http://public@domain/pre%23fix/42",
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			dsn, err := NewDsn(in)
			if err != nil {
				t.Skipf("NewDsn() rejects %q: %v", in, err)
			}
			// Callers dereference the result from a background goroutine, where
			// a nil URL takes the whole process down.
			apiURL := dsn.GetAPIURL()
			if apiURL == nil {
				t.Fatalf("GetAPIURL() = nil for %q, which NewDsn() accepted", in)
			}
			if !strings.HasSuffix(apiURL.Path, "/envelope/") {
				t.Errorf("GetAPIURL().Path = %q, want it to end in /envelope/", apiURL.Path)
			}
			if apiURL.RawQuery != "" || apiURL.Fragment != "" {
				t.Errorf("GetAPIURL() = %q, want no query (%q) and no fragment (%q)", apiURL, apiURL.RawQuery, apiURL.Fragment)
			}
		})
	}
}

func TestDsnStringRoundTrip(t *testing.T) {
	for name, tt := range dsnTests {
		t.Run(name, func(t *testing.T) {
			dsn, err := NewDsn(tt.in)
			if err != nil {
				t.Fatalf("NewDsn() error: %q", err)
			}
			reparsed, err := NewDsn(dsn.String())
			if err != nil {
				t.Fatalf("NewDsn(%q) error: %q", dsn.String(), err)
			}
			if diff := cmp.Diff(dsn, reparsed, cmp.AllowUnexported(Dsn{})); diff != "" {
				t.Errorf("re-parsing String() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type invalidDsnTest struct {
	in  string
	err string // expected substring of the error
}

var invalidDsnTests = map[string]invalidDsnTest{
	"Empty":     {"", "invalid scheme"},
	"NoScheme1": {"public:secret@:8888/42", "invalid scheme"},
	// FIXME: NoScheme2's error message is inconsistent with NoScheme1; consider
	// avoiding leaking errors from url.Parse.
	"NoScheme2":     {"://public:secret@:8888/42", "missing protocol scheme"},
	"NoPublicKey":   {"https://:secret@domain:8888/42", "empty username"},
	"NoHost":        {"https://public:secret@:8888/42", "empty host"},
	"NoProjectID1":  {"https://public:secret@domain:8888/", "empty project id"},
	"NoProjectID2":  {"https://public:secret@domain:8888", "empty project id"},
	"BadURL":        {"!@#$%^&*()", "invalid url"},
	"BadScheme":     {"ftp://public:secret@domain:8888/1", "invalid scheme"},
	"BadPort":       {"https://public:secret@domain:wat/42", "invalid port"},
	"TrailingSlash": {"https://public:secret@domain:8888/42/", "empty project id"},
}

func TestNewDsnInvalidInput(t *testing.T) {
	for name, tt := range invalidDsnTests {
		t.Run(name, func(t *testing.T) {
			_, err := NewDsn(tt.in)
			if err == nil {
				t.Fatalf("got nil, want error with %q", tt.err)
			}
			var dsnParseError *DsnParseError
			if !errors.As(err, &dsnParseError) {
				t.Errorf("got %T, want %T", err, (*DsnParseError)(nil))
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("%q does not contain %q", err.Error(), tt.err)
			}
		})
	}
}

func TestDsnSerializeDeserialize(t *testing.T) {
	url := "https://public:secret@domain:8888/foo/bar/42" //nolint:gosec // G101: not real credentials
	dsn, dsnErr := NewDsn(url)
	serialized, _ := json.Marshal(dsn)
	var deserialized Dsn
	unmarshalErr := json.Unmarshal(serialized, &deserialized)

	if unmarshalErr != nil {
		t.Error("expected dsn unmarshal to not return error")
	}
	if dsnErr != nil {
		t.Error("expected NewDsn to not return error")
	}
	expected := `"https://public:secret@domain:8888/foo/bar/42"` //nolint:gosec // G101: not real credentials
	if string(serialized) != expected {
		t.Errorf("Expected %s, got %s", expected, string(serialized))
	}
	if deserialized.String() != url {
		t.Errorf("Expected %s, got %s", url, deserialized.String())
	}
}

func TestDsnDeserializeInvalidJSON(t *testing.T) {
	var invalidJSON Dsn
	invalidJSONErr := json.Unmarshal([]byte(`"whoops`), &invalidJSON)
	var invalidDsn Dsn
	invalidDsnErr := json.Unmarshal([]byte(`"http://wat"`), &invalidDsn)

	if invalidJSONErr == nil {
		t.Error("expected dsn unmarshal to return error")
	}
	if invalidDsnErr == nil {
		t.Error("expected dsn unmarshal to return error")
	}
}

func TestRequestHeadersWithoutSecretKey(t *testing.T) {
	url := "https://public@domain/42"
	dsn, err := NewDsn(url)
	if err != nil {
		t.Fatal(err)
	}
	headers := dsn.RequestHeaders("sentry.go/1.0.0")
	authRegexp := regexp.MustCompile("^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=public$")

	if len(headers) != 2 {
		t.Error("expected request to have 2 headers")
	}
	if headers["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type to be application/json, got %s", headers["Content-Type"])
	}
	if authRegexp.FindStringIndex(headers["X-Sentry-Auth"]) == nil {
		t.Error("expected auth header to fulfill provided pattern")
	}
}

func TestRequestHeadersWithSecretKey(t *testing.T) {
	url := "https://public:secret@domain/42" //nolint:gosec // G101: not real credentials
	dsn, err := NewDsn(url)
	if err != nil {
		t.Fatal(err)
	}
	headers := dsn.RequestHeaders("sentry.go/1.0.0")
	authRegexp := regexp.MustCompile("^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=public, sentry_secret=secret$")

	if len(headers) != 2 {
		t.Error("expected request to have 2 headers")
	}
	if headers["Content-Type"] != "application/json" {
		t.Errorf("Expected Content-Type to be application/json, got %s", headers["Content-Type"])
	}
	if authRegexp.FindStringIndex(headers["X-Sentry-Auth"]) == nil {
		t.Error("expected auth header to fulfill provided pattern")
	}
}

func TestGetScheme(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"http://public:secret@domain/42", "http"},
		{"https://public:secret@domain/42", "https"},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetScheme() != tt.want {
			t.Errorf("Expected scheme %s, got %s", tt.want, dsn.GetScheme())
		}
	}
}

func TestGetPublicKey(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"https://public:secret@domain/42", "public"},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetPublicKey() != tt.want {
			t.Errorf("Expected public key %s, got %s", tt.want, dsn.GetPublicKey())
		}
	}
}

func TestGetSecretKey(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"https://public:secret@domain/42", "secret"},
		{"https://public@domain/42", ""},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetSecretKey() != tt.want {
			t.Errorf("Expected secret key %s, got %s", tt.want, dsn.GetSecretKey())
		}
	}
}

func TestGetHost(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"http://public:secret@domain/42", "domain"},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetHost() != tt.want {
			t.Errorf("Expected host %s, got %s", tt.want, dsn.GetHost())
		}
	}
}

func TestGetPort(t *testing.T) {
	tests := []struct {
		dsn  string
		want int
	}{
		{"https://public:secret@domain/42", 443},
		{"http://public:secret@domain/42", 80},
		{"https://public:secret@domain:3000/42", 3000},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetPort() != tt.want {
			t.Errorf("Expected port %d, got %d", tt.want, dsn.GetPort())
		}
	}
}

func TestGetPath(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"https://public:secret@domain/42", ""},
		{"https://public:secret@domain/foo/bar/42", "/foo/bar"},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetPath() != tt.want {
			t.Errorf("Expected path %s, got %s", tt.want, dsn.GetPath())
		}
	}
}

func TestGetProjectID(t *testing.T) {
	tests := []struct {
		dsn  string
		want string
	}{
		{"https://public:secret@domain/42", "42"},
	}
	for _, tt := range tests {
		dsn, err := NewDsn(tt.dsn)
		if err != nil {
			t.Fatal(err)
		}
		if dsn.GetProjectID() != tt.want {
			t.Errorf("Expected project ID %s, got %s", tt.want, dsn.GetProjectID())
		}
	}
}
