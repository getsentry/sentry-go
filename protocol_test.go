package sentry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDsnParsing(t *testing.T) {
	url := "https://username:password@domain:8888/foo/bar/23"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	assert.Equal(t, SchemeHTTPS, dsn.scheme)
	assert.Equal(t, "username", dsn.publicKey)
	assert.Equal(t, "password", dsn.secretKey)
	assert.Equal(t, "domain", dsn.host)
	assert.Equal(t, 8888, dsn.port)
	assert.Equal(t, "/foo/bar", dsn.path)
	assert.Equal(t, 23, dsn.projectID)
	assert.Equal(t, url, dsn.ToString())
}

func TestDsnDefaultPort(t *testing.T) {
	assert.Equal(t, 1337, Dsn{port: 1337}.Port())
	assert.Equal(t, 1337, Dsn{scheme: "https", port: 1337}.Port())
	assert.Equal(t, 443, Dsn{scheme: "https"}.Port())
	assert.Equal(t, 80, Dsn{scheme: "http"}.Port())
}

func TestDsnSerializeDeserialize(t *testing.T) {
	url := "https://username:password@domain:8888/foo/bar/23"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	serialized, _ := json.Marshal(dsn)
	assert.Equal(t, "\"https://username:password@domain:8888/foo/bar/23\"", string(serialized))
	var deserialized Dsn
	err = json.Unmarshal(serialized, &deserialized)
	if err != nil {
		assert.Fail(t, "Failed to unmarshal DSN JSON")
	}
	assert.Equal(t, url, deserialized.ToString())
}

func TestDsnNoPort(t *testing.T) {
	url := "https://username@domain/42"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	assert.Equal(t, 443, dsn.Port())
	assert.Equal(t, url, dsn.ToString())
	assert.Equal(t, "https://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestInsecureDsnNoPort(t *testing.T) {
	url := "http://username@domain/42"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	assert.Equal(t, 80, dsn.Port())
	assert.Equal(t, url, dsn.ToString())
	assert.Equal(t, "http://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestDsnNoPassword(t *testing.T) {
	url := "https://username@domain:8888/42"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	assert.Equal(t, url, dsn.ToString())
	assert.Equal(t, "https://domain:8888/api/42/store/", dsn.StoreAPIURL().String())
}

func TestDsnHttpUrl(t *testing.T) {
	url := "http://username@domain:8888/42"
	dsn, err := NewDsn(url)
	if err != nil {
		assert.Fail(t, "DsnParseError")
	}
	assert.Equal(t, url, dsn.ToString())
}

func TestDsnInvalidUrl(t *testing.T) {
	_, err := NewDsn("!@#$%^&*()")
	if _, ok := err.(*DsnParseError); !ok {
		assert.Fail(t, "should return DsnParseError")
	}
	assert.Equal(t, "DsnParseError: invalid url", err.Error())
}

func TestDsnInvalidScheme(t *testing.T) {
	_, err := NewDsn("ftp://username:password@domain:8888/1")
	if _, ok := err.(*DsnParseError); !ok {
		assert.Fail(t, "should return DsnParseError")
	}
	assert.Equal(t, "DsnParseError: invalid scheme", err.Error())
}
func TestDsnNoUsername(t *testing.T) {
	_, err := NewDsn("https://:password@domain:8888/23")
	if _, ok := err.(*DsnParseError); !ok {
		assert.Fail(t, "should return DsnParseError")
	}
	assert.Equal(t, "DsnParseError: empty username", err.Error())
}

func TestDsnNoHost(t *testing.T) {
	_, err := NewDsn("https://username:password@:8888/42")
	if _, ok := err.(*DsnParseError); !ok {
		assert.Fail(t, "should return DsnParseError")
	}
	assert.Equal(t, "DsnParseError: empty host", err.Error())
}

func TestDsnNoProjectId(t *testing.T) {
	_, err := NewDsn("https://username:password@domain:8888/")
	if _, ok := err.(*DsnParseError); !ok {
		assert.Fail(t, "should return DsnParseError")
	}
	assert.Equal(t, "DsnParseError: empty project id", err.Error())
}
