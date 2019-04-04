package sentry

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/stretchr/testify/suite"
)

type DsnSuite struct {
	suite.Suite
}

func TestDsnSuite(t *testing.T) {
	suite.Run(t, new(DsnSuite))
}

func (suite *DsnSuite) TestDsnParsing() {
	url := "https://username:password@domain:8888/foo/bar/23"

	dsn, err := NewDsn(url)

	suite.Nil(err)
	suite.Equal(SchemeHTTPS, dsn.scheme)
	suite.Equal("username", dsn.publicKey)
	suite.Equal("password", dsn.secretKey)
	suite.Equal("domain", dsn.host)
	suite.Equal(8888, dsn.port)
	suite.Equal("/foo/bar", dsn.path)
	suite.Equal(23, dsn.projectID)
	suite.Equal(url, dsn.ToString())
}

func (suite *DsnSuite) TestDsnDefaultPort() {
	suite.Equal(1337, Dsn{port: 1337}.Port())
	suite.Equal(1337, Dsn{scheme: "https", port: 1337}.Port())
	suite.Equal(443, Dsn{scheme: "https"}.Port())
	suite.Equal(80, Dsn{scheme: "http"}.Port())
	suite.Equal(80, Dsn{scheme: "shrug"}.Port())
}

func (suite *DsnSuite) TestDsnSerializeDeserialize() {
	url := "https://username:password@domain:8888/foo/bar/23"

	dsn, dsnErr := NewDsn(url)
	serialized, _ := json.Marshal(dsn)
	var deserialized Dsn
	unmarshalErr := json.Unmarshal(serialized, &deserialized)

	suite.Nil(unmarshalErr)
	suite.Nil(dsnErr)
	suite.Equal("\"https://username:password@domain:8888/foo/bar/23\"", string(serialized))
	suite.Equal(url, deserialized.ToString())
}

func (suite *DsnSuite) TestDsnDeserializeInvalidJSON() {
	var invalidJSON Dsn
	invalidJSONErr := json.Unmarshal([]byte("\"whoops"), &invalidJSON)

	var invalidDsn Dsn
	invalidDsnErr := json.Unmarshal([]byte("\"http://wat\""), &invalidDsn)

	suite.Error(invalidJSONErr)
	suite.Error(invalidDsnErr)
}

func (suite *DsnSuite) TestDsnNoInput() {
	dsn, err := NewDsn("")
	suite.Nil(dsn, "Should return nil pointer to dsn")
	suite.Nil(err, "Shouldnt throw error")
}

func (suite *DsnSuite) TestDsnNoPort() {
	url := "https://username@domain/42"

	dsn, err := NewDsn(url)

	suite.Nil(err)
	suite.Equal(443, dsn.Port())
	suite.Equal(url, dsn.ToString())
	suite.Equal("https://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func (suite *DsnSuite) TestInsecureDsnNoPort() {
	url := "http://username@domain/42"

	dsn, err := NewDsn(url)

	suite.Nil(err)
	suite.Equal(80, dsn.Port())
	suite.Equal(url, dsn.ToString())
	suite.Equal("http://domain/api/42/store/", dsn.StoreAPIURL().String())
}

func (suite *DsnSuite) TestDsnNoPassword() {
	url := "https://username@domain:8888/42"

	dsn, err := NewDsn(url)

	suite.Nil(err)
	suite.Equal(url, dsn.ToString())
	suite.Equal("https://domain:8888/api/42/store/", dsn.StoreAPIURL().String())
}

func (suite *DsnSuite) TestDsnHttpUrl() {
	url := "http://username@domain:8888/42"

	dsn, err := NewDsn(url)

	suite.Nil(err)
	suite.Equal(url, dsn.ToString())
}

func (suite *DsnSuite) TestDsnInvalidUrl() {
	_, err := NewDsn("!@#$%^&*()")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "invalid url")
}

func (suite *DsnSuite) TestDsnInvalidScheme() {
	_, err := NewDsn("ftp://username:password@domain:8888/1")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "invalid scheme")
}
func (suite *DsnSuite) TestDsnNoUsername() {
	_, err := NewDsn("https://:password@domain:8888/23")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "empty username")
}

func (suite *DsnSuite) TestDsnNoHost() {
	_, err := NewDsn("https://username:password@:8888/42")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "empty host")
}

func (suite *DsnSuite) TestDsnInvalidPort() {
	_, err := NewDsn("https://username:password@domain:wat/42")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "invalid port")
}

func (suite *DsnSuite) TestDsnNoProjectId() {
	_, err := NewDsn("https://username:password@domain:8888/")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "empty project id")
}

func (suite *DsnSuite) TestDsnInvalidProjectId() {
	_, err := NewDsn("https://username:password@domain:8888/wbvdf7^W#$")
	_, ok := err.(*DsnParseError)

	suite.True(ok)
	suite.Contains(err.Error(), "invalid project id")
}

func (suite *DsnSuite) TestRequestHeaders() {
	url := "https://username@domain:8888/23"
	dsn, _ := NewDsn(url)
	headers := dsn.RequestHeaders()
	authFormat := "^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=username$"

	suite.Len(headers, 2)
	suite.Equal("application/json", headers["Content-Type"])
	suite.Regexp(regexp.MustCompile(authFormat), headers["X-Sentry-Auth"])
}

func (suite *DsnSuite) TestRequestHeadersWithSecret() {
	url := "https://username:secret@domain:8888/23"
	dsn, _ := NewDsn(url)
	headers := dsn.RequestHeaders()
	authFormat := "^Sentry sentry_version=7, sentry_timestamp=\\d+, " +
		"sentry_client=sentry.go/.+, sentry_key=username, sentry_secret=secret$"

	suite.Len(headers, 2)
	suite.Equal("application/json", headers["Content-Type"])
	suite.Regexp(regexp.MustCompile(authFormat), headers["X-Sentry-Auth"])
}
