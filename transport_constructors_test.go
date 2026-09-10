package sentry_test

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/stretchr/testify/require"
)

type configuredEnvelopeTransport interface {
	SendEnvelope(*protocol.Envelope) error
	Flush(time.Duration) bool
	Close()
}

type constructorRoundTripper func(*http.Request) (*http.Response, error)

func (f constructorRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestTransportConstructorsConfigureReports(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		newTransport func(sentry.TransportOptions) configuredEnvelopeTransport
	}{
		{"async", func(cfg sentry.TransportOptions) configuredEnvelopeTransport { return sentry.NewHTTPTransport(cfg) }},
		{"sync", func(cfg sentry.TransportOptions) configuredEnvelopeTransport { return sentry.NewHTTPSyncTransport(cfg) }},
	} {
		for _, disabled := range []bool{false, true} {
			name := test.name
			if disabled {
				name += "/reports-disabled"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var mu sync.Mutex
				var bodies []string
				transport := test.newTransport(sentry.TransportOptions{
					Dsn: "https://public@example.com/1", DisableClientReports: disabled,
					HTTPTransport: constructorRoundTripper(func(request *http.Request) (*http.Response, error) {
						defer request.Body.Close()
						body, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						mu.Lock()
						defer mu.Unlock()
						bodies = append(bodies, string(body))
						status := http.StatusOK
						if len(bodies) == 1 {
							status = http.StatusInternalServerError
						}
						return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}"))}, nil
					}),
				})
				t.Cleanup(transport.Close)
				require.NoError(t, transport.SendEnvelope(protocol.NewEnvelope(&protocol.EnvelopeHeader{},
					protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"failure"}`)))))
				require.True(t, transport.Flush(time.Second))
				mu.Lock()
				defer mu.Unlock()
				if disabled {
					require.Len(t, bodies, 1)
					return
				}
				require.Len(t, bodies, 2)
				require.Contains(t, bodies[1], `"type":"client_report"`)
				require.Contains(t, bodies[1], `"reason":"send_error","category":"error","quantity":1`)
				require.Contains(t, bodies[1], `"version":"`+sentry.SDKVersion+`"`)
			})
		}
	}
}
