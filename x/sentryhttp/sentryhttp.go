package sentryhttp

import (
	"context"
	"net/http"
)

func NewHandler(h http.Handler) http.Handler {
	// TODO: instrument handler to create spans and recover from panics.
	return h
}

func NewTransport(rt http.RoundTripper) http.RoundTripper {
	// TODO: instrument rt to create spans for outgoing requests.
	return rt
}

// defaultClient is the http.Client used by Get, Head, and Post.
//
// To customize the client, create a new http.Client and use NewTransport to
// wrap the client's transport.
var defaultClient = &http.Client{Transport: NewTransport(http.DefaultTransport)}

// Get issues a GET to the specified URL. It is a shortcut for http.Get with a
// context.
//
// See the Go standard library documentation for net/http for details.
//
// When err is nil, resp always contains a non-nil resp.Body.
// Caller should close resp.Body when done reading from it.
//
// To make a custom request, create a client with a transport wrapped by
// NewTransport and use http.NewRequestWithContext and http.Client.Do.
func Get(ctx context.Context, url string) (resp *http.Response, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	return defaultClient.Do(req)
}
