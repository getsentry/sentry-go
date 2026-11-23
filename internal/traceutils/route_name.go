package traceutils

import (
	"net/http"
	"strings"
)

// GetHTTPSpanName grab needed fields from *http.Request to generate a span name for `http.server` span op.
func GetHTTPSpanName(r *http.Request) string {
	if r.Pattern != "" {
		// If value does not start with HTTP methods, add them.
		// The method and the path should be separated by a space.
		if !strings.Contains(r.Pattern, " ") {
			return r.Method + " " + r.Pattern
		}

		return r.Pattern
	}

	return r.Method + " " + r.URL.Path
}
