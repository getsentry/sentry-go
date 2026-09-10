package sentry_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const transportTestDSN = "https://public@example.com/1"

type observingEnvelopeTransport struct {
	sentry.Transport
	observed sentry.MockTransport
}

func (t *observingEnvelopeTransport) SendEnvelope(envelope *protocol.Envelope) error {
	// HTTP delivery may append reports to the item list. Keep the observation's
	// list separate; the existing headers and payloads are immutable.
	observation := *envelope
	observation.Items = append([]*protocol.EnvelopeItem(nil), envelope.Items...)
	if err := t.observed.SendEnvelope(&observation); err != nil {
		return err
	}
	return t.Transport.SendEnvelope(envelope)
}

func TestPublicTransportWrapping(t *testing.T) {
	t.Parallel()
	transport := &observingEnvelopeTransport{Transport: sentry.NewHTTPTransport(sentry.TransportOptions{
		Dsn: transportTestDSN, HTTPTransport: requestTransportFunc(func(request *http.Request) (*http.Response, error) {
			defer request.Body.Close()
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}),
	})}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn: transportTestDSN, Transport: transport, EnableTracing: true, TracesSampleRate: 1,
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)
	require.Same(t, transport, client.Transport)
	ctx, scope := sentry.WithScope(context.Background())
	ctx = sentry.ContextWithClient(ctx, client)
	scope.SetTag("capture", "before")
	scope.AddAttachment(&sentry.Attachment{Filename: "test.txt", Payload: []byte("attachment")})
	sentry.CaptureMessage(ctx, "wrapped event")
	scope.SetTag("capture", "after")
	sentry.StartTransaction(ctx, "wrapped transaction").Finish()
	sentry.NewLogger(ctx).Info().Emit("wrapped log")
	sentry.NewMeter(ctx).Count("wrapped.counter", 1)
	sentry.CaptureCheckIn(ctx, &sentry.CheckIn{MonitorSlug: "job", Status: sentry.CheckInStatusOK}, nil)
	require.True(t, client.Flush(time.Second))
	types := make(map[protocol.EnvelopeItemType]bool)
	for _, envelope := range transport.observed.Envelopes() {
		for _, item := range envelope.Items {
			types[item.Header.Type] = true
		}
	}
	for _, kind := range []protocol.EnvelopeItemType{
		protocol.EnvelopeItemTypeEvent, protocol.EnvelopeItemTypeTransaction, protocol.EnvelopeItemTypeLog,
		protocol.EnvelopeItemTypeTraceMetric, protocol.EnvelopeItemTypeCheckIn, protocol.EnvelopeItemTypeAttachment,
	} {
		assert.True(t, types[kind], "missing %s", kind)
	}
	for _, event := range transport.observed.Events() {
		if event.Message == "wrapped event" {
			assert.Equal(t, "before", event.Tags["capture"])
		}
	}
}

func TestClientReportsWithoutOrdinaryTelemetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport := &minimalEnvelopeTransport{}
		client, err := sentry.NewClient(sentry.ClientOptions{
			Transport:  transport,
			BeforeSend: func(*sentry.Event, *sentry.EventHint) *sentry.Event { return nil },
		})
		require.NoError(t, err)
		t.Cleanup(client.Close)
		client.CaptureMessage(context.Background(), "discard")
		time.Sleep(31 * time.Second)
		synctest.Wait()
		envelopes := transport.captured.Envelopes()
		require.Len(t, envelopes, 1)
		assert.Empty(t, envelopes[0].Header.EventID)
		assert.Equal(t, protocol.EnvelopeItemTypeClientReport, envelopes[0].Items[0].Header.Type)
		assert.Empty(t, transport.captured.Events())
	})
}

// A custom transport needs only delivery and lifecycle methods.
type minimalEnvelopeTransport struct{ captured sentry.MockTransport }

func (t *minimalEnvelopeTransport) SendEnvelope(e *protocol.Envelope) error {
	return t.captured.SendEnvelope(e)
}

func (*minimalEnvelopeTransport) Flush(time.Duration) bool              { return true }
func (*minimalEnvelopeTransport) FlushWithContext(context.Context) bool { return true }
func (*minimalEnvelopeTransport) Close()                                {}

type requestTransportFunc func(*http.Request) (*http.Response, error)

func (f requestTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportConstructorsConfigureReports(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		newTransport func(sentry.TransportOptions) sentry.Transport
	}{
		{"async", sentry.NewHTTPTransport},
		{"sync", sentry.NewHTTPSyncTransport},
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
					HTTPTransport: requestTransportFunc(func(request *http.Request) (*http.Response, error) {
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

func TestHTTPSyncTransportDeliversDuringCapture(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var requests atomic.Int32
		transport := sentry.NewHTTPSyncTransport(sentry.TransportOptions{
			Dsn: transportTestDSN, DisableClientReports: true, Timeout: 5 * time.Second,
			HTTPTransport: requestTransportFunc(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				if requests.Add(1) > 1 {
					// The batched log blocks until its request times out.
					<-request.Context().Done()
					return nil, request.Context().Err()
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			}),
		})
		client, err := sentry.NewClient(sentry.ClientOptions{Dsn: transportTestDSN, Transport: transport})
		require.NoError(t, err)
		ctx := sentry.ContextWithClient(context.Background(), client)

		require.NotNil(t, sentry.CaptureMessage(ctx, "sync"))
		assert.EqualValues(t, 1, requests.Load(), "capture must deliver before returning")

		sentry.NewLogger(ctx).Info().Emit("batched")
		start := time.Now()
		assert.False(t, client.Flush(time.Second))
		assert.Equal(t, time.Second, time.Since(start), "flush must honor its deadline")

		client.Close()
		assert.Nil(t, sentry.CaptureMessage(ctx, "after close"))
		time.Sleep(5 * time.Second) // let the timed-out log request finish
	})
}
