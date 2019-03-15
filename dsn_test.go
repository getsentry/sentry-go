package sentry

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDsnParsing(t *testing.T) {
	assert := assert.New(t)
	url := "https://username:password@domain:8888/foo/bar/23"

	dsn, err := NewDsn(url)

	assert.Nil(err)
	assert.Equal(SchemeHTTPS, dsn.scheme)
	assert.Equal("username", dsn.publicKey)
	assert.Equal("password", dsn.secretKey)
	assert.Equal("domain", dsn.host)
	assert.Equal(8888, dsn.port)
	assert.Equal("/foo/bar", dsn.path)
	assert.Equal(23, dsn.projectID)
	assert.Equal(url, dsn.ToString())
}

func TestDsnDefaultPort(t *testing.T) {
	assert := assert.New(t)

	assert.Equal(1337, Dsn{port: 1337}.Port())
	assert.Equal(1337, Dsn{scheme: "https", port: 1337}.Port())
	assert.Equal(443, Dsn{scheme: "https"}.Port())
	assert.Equal(80, Dsn{scheme: "http"}.Port())
}

func TestDsnSerializeDeserialize(t *testing.T) {
	assert := assert.New(t)
	url := "https://username:password@domain:8888/foo/bar/23"

	dsn, dsnErr := NewDsn(url)
	serialized, _ := json.Marshal(dsn)
	var deserialized Dsn
	unmarshalErr := json.Unmarshal(serialized, &deserialized)

	assert.Nil(unmarshalErr)
	assert.Nil(dsnErr)
	assert.Equal("\"https://username:password@domain:8888/foo/bar/23\"", string(serialized))
	assert.Equal(url, deserialized.ToString())
}

func TestDsnNoPort(t *testing.T) {
	assert := assert.New(t)
	url := "https://username@domain/42"

	dsn, err := NewDsn(url)

	assert.Nil(err)
	assert.Equal(443, dsn.Port())
	assert.Equal(url, dsn.ToString())
	assert.Equal("https://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestInsecureDsnNoPort(t *testing.T) {
	assert := assert.New(t)
	url := "http://username@domain/42"

	dsn, err := NewDsn(url)

	assert.Nil(err)
	assert.Equal(80, dsn.Port())
	assert.Equal(url, dsn.ToString())
	assert.Equal("http://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func TestDsnNoPassword(t *testing.T) {
	assert := assert.New(t)
	url := "https://username@domain:8888/42"

	dsn, err := NewDsn(url)

	assert.Nil(err)
	assert.Equal(url, dsn.ToString())
	assert.Equal("https://domain:8888/api/42/store/", dsn.StoreAPIURL().String())
}

func TestDsnHttpUrl(t *testing.T) {
	assert := assert.New(t)
	url := "http://username@domain:8888/42"

	dsn, err := NewDsn(url)

	assert.Nil(err)
	assert.Equal(url, dsn.ToString())
}

func TestDsnInvalidUrl(t *testing.T) {
	assert := assert.New(t)

	_, err := NewDsn("!@#$%^&*()")
	_, ok := err.(*DsnParseError)

	assert.True(ok)
	assert.Equal("DsnParseError: invalid url", err.Error())
}

func TestDsnInvalidScheme(t *testing.T) {
	assert := assert.New(t)

	_, err := NewDsn("ftp://username:password@domain:8888/1")
	_, ok := err.(*DsnParseError)

	assert.True(ok)
	assert.Equal("DsnParseError: invalid scheme", err.Error())
}
func TestDsnNoUsername(t *testing.T) {
	assert := assert.New(t)

	_, err := NewDsn("https://:password@domain:8888/23")
	_, ok := err.(*DsnParseError)

	assert.True(ok)
	assert.Equal("DsnParseError: empty username", err.Error())
}

func TestDsnNoHost(t *testing.T) {
	assert := assert.New(t)

	_, err := NewDsn("https://username:password@:8888/42")
	_, ok := err.(*DsnParseError)

	assert.True(ok)
	assert.Equal("DsnParseError: empty host", err.Error())
}

func TestDsnNoProjectId(t *testing.T) {
	assert := assert.New(t)

	_, err := NewDsn("https://username:password@domain:8888/")
	_, ok := err.(*DsnParseError)

	assert.True(ok)
	assert.Equal("DsnParseError: empty project id", err.Error())
}
