package sentry

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestDsnParsing(t *testing.T) {
	url := "https://username:password@domain:8888/foo/bar/23"
	dsn, err := NewDsn(url)
	if err != nil {
		t.Error("expected dsn to be correctly created")
	}
	assertEqual(t, schemeHTTPS, dsn.scheme)
	assertEqual(t, dsn.publicKey, "username")
	assertEqual(t, dsn.secretKey, "password")
	assertEqual(t, dsn.host, "domain")
	assertEqual(t, dsn.path, "/foo/bar")
	assertEqual(t, dsn.String(), url)
	assertEqual(t, dsn.port, 8888)
	assertEqual(t, dsn.projectID, 23)
}

func TestDsnDefaultPort(t *testing.T) {
	dsn, _ := NewDsn("https://u@d:1337/23")
	assertEqual(t, dsn.port, 1337)
	dsn, _ = NewDsn("https://u@d/23")
	assertEqual(t, dsn.port, 443)
	dsn, _ = NewDsn("http://u@d/23")
	assertEqual(t, dsn.port, 80)
}

func TestDsnSerializeDeserialize(t *testing.T) {
	url := "https://username:password@domain:8888/foo/bar/23"
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
	assertEqual(t, "\"https://username:password@domain:8888/foo/bar/23\"", string(serialized))
	assertEqual(t, url, deserialized.String())
}

func TestDsnDeserializeInvalidJSON(t *testing.T) {
	var invalidJSON Dsn
	invalidJSONErr := json.Unmarshal([]byte("\"whoops"), &invalidJSON)
	var invalidDsn Dsn
	invalidDsnErr := json.Unmarshal([]byte("\"http://wat\""), &invalidDsn)

	if invalidJSONErr == nil {
		t.Error("expected dsn unmarshal to return error")
	}
	if invalidDsnErr == nil {
		t.Error("expected dsn unmarshal to return error")
	}
}

func TestValidDsnInsecure(t *testing.T) {
	url := "http://username@domain:8888/42"
	dsn, err := NewDsn(url)

	if err != nil {
		t.Error("expected dsn to be correctly created")
	}
	assertEqual(t, url, dsn.String())
}

func TestValidDsnNoPort(t *testing.T) {
	url := "http://username@domain/42"
	dsn, err := NewDsn(url)

	if err != nil {
		t.Error("expected dsn to be correctly created")
	}
	assertEqual(t, 80, dsn.port)
	assertEqual(t, url, dsn.String())
	assertEqual(t, "http://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestValidDsnInsecureNoPort(t *testing.T) {
	url := "https://username@domain/42"
	dsn, err := NewDsn(url)

	if err != nil {
		t.Error("expected dsn to be correctly created")
	}
	assertEqual(t, 443, dsn.port)
	assertEqual(t, url, dsn.String())
	assertEqual(t, "https://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestValidDsnNoPassword(t *testing.T) {
	url := "https://username@domain:8888/42"
	dsn, err := NewDsn(url)

	if err != nil {
		t.Error("expected dsn to be correctly created")
	}
	assertEqual(t, url, dsn.String())
	assertEqual(t, "https://domain:8888/api/42/store/", dsn.StoreAPIURL().String())
}

func TestInvalidDsnInvalidUrl(t *testing.T) {
	_, err := NewDsn("!@#$%^&*()")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "invalid url")
}

func TestInvalidDsnInvalidScheme(t *testing.T) {
	_, err := NewDsn("ftp://username:password@domain:8888/1")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "invalid scheme")
}

func TestInvalidDsnNoUsername(t *testing.T) {
	_, err := NewDsn("https://:password@domain:8888/23")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "empty username")
}

func TestInvalidDsnNoHost(t *testing.T) {
	_, err := NewDsn("https://username:password@:8888/42")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "empty host")
}

func TestInvalidDsnInvalidPort(t *testing.T) {
	_, err := NewDsn("https://username:password@domain:wat/42")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "invalid port")
}

func TestInvalidDsnNoProjectId(t *testing.T) {
	_, err := NewDsn("https://username:password@domain:8888/")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "empty project id")
}

func TestInvalidDsnInvalidProjectId(t *testing.T) {
	_, err := NewDsn("https://username:password@domain:8888/wbvdf7^W#$")
	_, ok := err.(*DsnParseError)

	if ok != true {
		t.Error("expected error to be of type DsnParseError")
	}
	assertStringContains(t, err.Error(), "invalid project id")
}

func TestRequestHeadersWithoutPassword(t *testing.T) {
	url := "https://username@domain:8888/23"
	dsn, _ := NewDsn(url)
	headers := dsn.RequestHeaders()
	authRegexp := regexp.MustCompile("^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=username$")

	if len(headers) != 2 {
		t.Error("expected request to have 2 headers")
	}
	assertEqual(t, "application/json", headers["Content-Type"])
	if authRegexp.FindStringIndex(headers["X-Sentry-Auth"]) == nil {
		t.Error("expected auth header to fulfill provided pattern")
	}
}

func TestRequestHeadersWithPassword(t *testing.T) {
	url := "https://username:secret@domain:8888/23"
	dsn, _ := NewDsn(url)
	headers := dsn.RequestHeaders()
	authRegexp := regexp.MustCompile("^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=username, sentry_secret=secret$")

	if len(headers) != 2 {
		t.Error("expected request to have 2 headers")
	}
	assertEqual(t, "application/json", headers["Content-Type"])
	if authRegexp.FindStringIndex(headers["X-Sentry-Auth"]) == nil {
		t.Error("expected auth header to fulfill provided pattern")
	}
}
