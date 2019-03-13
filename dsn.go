package sentry

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
)

type Scheme string

const (
	SchemeHTTP  Scheme = "http"
	SchemeHTTPS Scheme = "https"
)

func (scheme Scheme) DefaultPort() int {
	switch scheme {
	case SchemeHTTPS:
		return 443
	case SchemeHTTP:
		return 80
	default:
		return 80
	}
}

type DsnParseError struct {
	Message string
}

func (e *DsnParseError) Error() string {
	return "DsnParseError: " + e.Message
}

type Dsn struct {
	scheme    Scheme
	publicKey string
	secretKey string
	host      string
	port      int
	path      string
	projectID int
}

func NewDsn(rawURL string) (*Dsn, error) {
	// Parse
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, &DsnParseError{"invalid url"}
	}

	// Scheme
	var scheme Scheme
	switch parsedURL.Scheme {
	case "http":
		scheme = SchemeHTTP
	case "https":
		scheme = SchemeHTTPS
	default:
		return nil, &DsnParseError{"invalid scheme"}
	}

	// PublicKey
	publicKey := parsedURL.User.Username()
	if publicKey == "" {
		return nil, &DsnParseError{"empty username"}
	}

	// SecretKey
	var secretKey string
	if parsedSecretKey, ok := parsedURL.User.Password(); ok {
		secretKey = parsedSecretKey
	}

	// Host
	host := parsedURL.Hostname()
	if host == "" {
		return nil, &DsnParseError{"empty host"}
	}

	// Port
	var port int
	if parsedURL.Port() != "" {
		parsedPort, err := strconv.Atoi(parsedURL.Port())
		if err != nil {
			return nil, &DsnParseError{"invalid port"}
		}
		port = parsedPort
	}

	// ProjectID
	if len(parsedURL.Path) == 0 || parsedURL.Path == "/" {
		return nil, &DsnParseError{"empty project id"}
	}
	pathSegments := strings.Split(parsedURL.Path[1:], "/")
	projectID, err := strconv.Atoi(pathSegments[len(pathSegments)-1])
	if err != nil {
		return nil, &DsnParseError{"invalid project id"}
	}

	// Path
	var path string
	if len(pathSegments) > 1 {
		path = "/" + strings.Join(pathSegments[0:len(pathSegments)-1], "/")
	}

	return &Dsn{
		scheme:    scheme,
		publicKey: publicKey,
		secretKey: secretKey,
		host:      host,
		port:      port,
		path:      path,
		projectID: projectID,
	}, nil
}

func (dsn Dsn) Port() int {
	if dsn.port == 0 {
		return dsn.scheme.DefaultPort()
	}
	return dsn.port
}

func (dsn Dsn) ToString() string {
	var url string
	url += fmt.Sprintf("%s://%s", dsn.scheme, dsn.publicKey)
	if dsn.secretKey != "" {
		url += fmt.Sprintf(":%s", dsn.secretKey)
	}
	url += fmt.Sprintf("@%s", dsn.host)
	if dsn.Port() != dsn.scheme.DefaultPort() {
		url += fmt.Sprintf(":%d", dsn.Port())
	}
	if dsn.path != "" {
		url += dsn.path
	}
	url += fmt.Sprintf("/%d", dsn.projectID)
	return url
}

func (dsn Dsn) StoreAPIURL() *url.URL {
	var rawURL string
	rawURL += fmt.Sprintf("%s://%s", dsn.scheme, dsn.host)
	if dsn.Port() != dsn.scheme.DefaultPort() {
		rawURL += fmt.Sprintf(":%d", dsn.Port())
	}
	rawURL += fmt.Sprintf("/api/%d/store/", dsn.projectID)
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		log.Fatalf("DsnParseError: invalid url.\n%s", err)
	}
	return parsedURL
}

func (dsn Dsn) MarshalJSON() ([]byte, error) {
	return json.Marshal(dsn.ToString())
}

func (dsn *Dsn) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return err
	}
	newDsn, err := NewDsn(str)
	*dsn = *newDsn
	if err != nil {
		return err
	}
	return nil
}
