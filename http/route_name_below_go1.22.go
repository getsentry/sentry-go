//go:build !go1.22

package sentryhttp

import "net/http"

func getHTTPSpanName(r *http.Request) string {
	return r.Method + " " + r.URL.Path
}
