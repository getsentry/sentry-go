//go:build !go1.22

package sentryhttp

import (
	"net/http"
	"net/url"
	"testing"
)

func TestGetHTTPSpanName(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "Without Pattern",
			got:  getHTTPSpanName(&http.Request{Method: "GET", URL: &url.URL{Path: "/"}}),
			want: "GET /",
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
