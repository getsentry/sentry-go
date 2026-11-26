package sentry_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/sentrytest"
	"github.com/google/go-cmp/cmp"
)

func TestIsLocalhostAttribute(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		host       string
		remoteAddr string
		headers    map[string]string
		want       *bool
	}{
		{name: "no request"},
		{name: "remote host", host: "example.com", remoteAddr: "192.0.2.1:1234", want: new(false)},
		{name: "loopback IPv4", host: "example.com", remoteAddr: "127.0.0.1:1234", want: new(true)},
		{name: "loopback IPv6", host: "example.com", remoteAddr: "[::1]:1234", want: new(true)},
		{name: "localhost host", host: "localhost:8080", remoteAddr: "192.0.2.1:1234", want: new(true)},
		{name: "localhost subdomain", host: "api.localhost", remoteAddr: "192.0.2.1:1234", want: new(true)},
		{name: "loopback host", host: "127.0.0.1:8080", remoteAddr: "192.0.2.1:1234", want: new(true)},
		{name: "lookalike host", host: "localhost.example.com", remoteAddr: "192.0.2.1:1234", want: new(false)},
		{
			name: "forwarded host", host: "example.com", remoteAddr: "192.0.2.1:1234",
			headers: map[string]string{"X-Forwarded-Host": "localhost:3000"}, want: new(true),
		},
		{
			name: "forwarded subdomain", host: "example.com", remoteAddr: "192.0.2.1:1234",
			headers: map[string]string{"X-Forwarded-Host": "api.localhost"}, want: new(false),
		},
		{
			name: "remote client behind local proxy", host: "example.com", remoteAddr: "127.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "203.0.113.5, 127.0.0.1"}, want: new(false),
		},
		{
			name: "local client behind proxy", host: "example.com", remoteAddr: "192.0.2.1:1234",
			headers: map[string]string{"X-Forwarded-For": "127.0.0.1"}, want: new(true),
		},
		{
			name: "real IP", host: "example.com", remoteAddr: "192.0.2.1:1234",
			headers: map[string]string{"X-Real-Ip": "::1"}, want: new(true),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sentrytest.Run(t, func(t *testing.T, f *sentrytest.Fixture) {
				if test.host != "" {
					r := httptest.NewRequest(http.MethodGet, "/", nil)
					r.Host = test.host
					r.RemoteAddr = test.remoteAddr
					for k, v := range test.headers {
						r.Header.Set(k, v)
					}
					f.Scope.SetRequest(r)
				}
				sentry.NewLogger(f.Context).Info().Emit("log")
				sentry.NewMeter(f.Context).Count("metric", 1)
				f.Flush()

				var got []*bool
				for _, event := range f.Events() {
					for _, l := range event.Logs {
						got = append(got, isLocalhost(l.Attributes))
					}
					for _, m := range event.Metrics {
						got = append(got, isLocalhost(m.Attributes))
					}
				}
				if diff := cmp.Diff([]*bool{test.want, test.want}, got); diff != "" {
					t.Errorf("sentry.is_localhost mismatch (-want +got):\n%s", diff)
				}
			})
		})
	}
}

func isLocalhost(attrs map[string]attribute.Value) *bool {
	v, ok := attrs["sentry.is_localhost"]
	if !ok {
		return nil
	}
	return new(v.AsBool())
}
