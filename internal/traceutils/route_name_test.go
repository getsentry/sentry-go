package traceutils

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestGetHTTPSpanName(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "Without Pattern",
			got:  GetHTTPSpanName(&http.Request{Method: "GET", URL: &url.URL{Path: "/"}}),
			want: "GET /",
		},
		{
			name: "Pattern with method",
			got:  GetHTTPSpanName(&http.Request{Method: "GET", URL: &url.URL{Path: "/"}, Pattern: "POST /foo/{bar}"}),
			want: "POST /foo/{bar}",
		},
		{
			name: "Pattern without method",
			got:  GetHTTPSpanName(&http.Request{Method: "GET", URL: &url.URL{Path: "/"}, Pattern: "/foo/{bar}"}),
			want: "GET /foo/{bar}",
		},
		{
			name: "Pattern without slash",
			got:  GetHTTPSpanName(&http.Request{Method: "GET", URL: &url.URL{Path: "/"}, Pattern: "example.com/"}),
			want: "GET example.com/",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %q; want %q", tt.got, tt.want)
			}
		})
	}
}

func TestGetHTTPTransactionSource(t *testing.T) {
	tests := []struct {
		name    string
		request *http.Request
		want    sentry.TransactionSource
	}{
		{
			name:    "Without Pattern returns SourceURL",
			request: &http.Request{Method: "GET", URL: &url.URL{Path: "/users/123"}},
			want:    sentry.SourceURL,
		},
		{
			name:    "With Pattern returns SourceRoute",
			request: &http.Request{Method: "GET", URL: &url.URL{Path: "/users/123"}, Pattern: "GET /users/{id}"},
			want:    sentry.SourceRoute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetHTTPTransactionSource(tt.request); got != tt.want {
				t.Errorf("GetHTTPTransactionSource() = %q; want %q", got, tt.want)
			}
		})
	}
}
