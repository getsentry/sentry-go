package sentry_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
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
	ctx, scope := sentry.WithIsolationScope(context.Background())
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
		transport := &sentry.MockTransport{}
		client, err := sentry.NewClient(sentry.ClientOptions{
			Transport:  transport,
			BeforeSend: func(*sentry.Event, *sentry.EventHint) *sentry.Event { return nil },
		})
		require.NoError(t, err)
		t.Cleanup(client.Close)
		client.CaptureMessage(context.Background(), "discard")
		time.Sleep(31 * time.Second)
		synctest.Wait()
		envelopes := transport.Envelopes()
		require.Len(t, envelopes, 1)
		assert.Empty(t, envelopes[0].Header.EventID)
		assert.Equal(t, protocol.EnvelopeItemTypeClientReport, envelopes[0].Items[0].Header.Type)
		assert.Empty(t, transport.Events())
	})
}

type requestTransportFunc func(*http.Request) (*http.Response, error)

func (f requestTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPublicTransportRequestTimeoutAndHTTPClientPrecedence(t *testing.T) {
	for _, constructor := range []func(sentry.TransportOptions) sentry.Transport{sentry.NewHTTPTransport, sentry.NewHTTPSyncTransport} {
		synctest.Test(t, func(t *testing.T) {
			deadlineSeen := make(chan time.Duration, 1)
			httpClient := &http.Client{Transport: requestTransportFunc(func(request *http.Request) (*http.Response, error) {
				deadline, ok := request.Context().Deadline()
				if ok {
					deadlineSeen <- time.Until(deadline)
				}
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			transport := constructor(sentry.TransportOptions{
				Dsn: transportTestDSN, HTTPClient: httpClient, Timeout: 50 * time.Millisecond,
				DisableClientReports: true,
				HTTPTransport: requestTransportFunc(func(*http.Request) (*http.Response, error) {
					t.Error("HTTPTransport should be ignored when HTTPClient is supplied")
					return nil, errors.New("unexpected RoundTrip")
				}),
			})
			t.Cleanup(transport.Close)
			_ = transport.SendEnvelope(protocol.NewEnvelope(&protocol.EnvelopeHeader{}, protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{}`))))
			require.True(t, transport.Flush(time.Second))
			assert.Equal(t, 50*time.Millisecond, <-deadlineSeen)
			assert.Zero(t, httpClient.Timeout, "supplied client must not be modified")
		})
	}
}
