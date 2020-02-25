package sentry

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestNewRequest(t *testing.T) {
	const payload = `{"test_data": true}`
	got := NewRequest(httptest.NewRequest("POST", "/test/?q=sentry", strings.NewReader(payload)))
	want := &Request{
		URL:         "http://example.com/test/",
		Method:      "POST",
		Data:        "",
		QueryString: "q=sentry",
		Cookies:     "",
		Headers: map[string]string{
			"Host": "example.com",
		},
		Env: map[string]string{
			"REMOTE_ADDR": "192.0.2.1",
			"REMOTE_PORT": "1234",
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("Request mismatch (-want +got):\n%s", diff)
	}
}
