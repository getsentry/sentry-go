package sentry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpDeliveryTestTransport interface {
	SendEnvelope(*protocol.Envelope) error
	FlushWithContext(context.Context) bool
	Close()
}

func newTestHTTPDelivery(async bool, options TransportOptions, aggregator *report.Aggregator) httpDeliveryTestTransport {
	var recorder report.ClientReportRecorder
	var provider report.ClientReportProvider
	if aggregator != nil {
		recorder, provider = aggregator, aggregator
	}
	sdkInfo := func() *protocol.SdkInfo {
		return &protocol.SdkInfo{Name: "test-delivery", Version: "1.0"}
	}
	if async {
		return newHTTPTransport(options, recorder, provider, sdkInfo)
	}
	return newHTTPSyncTransport(options, recorder, provider, sdkInfo)
}

type deliveryRoundTripper func(*http.Request) (*http.Response, error)

func (f deliveryRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHTTPDeliveryFlushReports(t *testing.T) {
	t.Parallel()
	for _, async := range []bool{false, true} {
		name := "sync"
		if async {
			name = "async"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var bodies []string
			transport := newTestHTTPDelivery(async, TransportOptions{
				Dsn: "https://public@example.com/1",
				HTTPTransport: deliveryRoundTripper(func(request *http.Request) (*http.Response, error) {
					defer request.Body.Close()
					body, err := io.ReadAll(request.Body)
					if err != nil {
						return nil, err
					}
					mu.Lock()
					bodies = append(bodies, string(body))
					mu.Unlock()
					status := http.StatusOK
					if strings.Contains(string(body), "failed delivery") {
						status = http.StatusInternalServerError
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("{}"))}, nil
				}),
			}, report.NewAggregator())
			t.Cleanup(transport.Close)
			// Flush must deliver the loss report even when no later event is sent.
			_ = transport.SendEnvelope(protocol.NewEnvelope(&protocol.EnvelopeHeader{},
				protocol.NewEnvelopeItem(protocol.EnvelopeItemTypeEvent, []byte(`{"message":"failed delivery"}`))))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.True(t, transport.FlushWithContext(ctx))
			mu.Lock()
			defer mu.Unlock()
			require.Len(t, bodies, 2)
			lines := strings.Split(strings.TrimSpace(bodies[1]), "\n")
			require.Len(t, lines, 3)
			var header protocol.EnvelopeHeader
			require.NoError(t, json.Unmarshal([]byte(lines[0]), &header))
			assert.Equal(t, &protocol.SdkInfo{Name: "test-delivery", Version: "1.0"}, header.Sdk)
			var payload report.ClientReport
			require.NoError(t, json.Unmarshal([]byte(lines[2]), &payload))
			assert.Equal(t, []report.DiscardedEvent{{Reason: report.ReasonSendError, Category: protocol.CategoryError, Quantity: 1}}, payload.DiscardedEvents)
		})
	}
}

func TestHTTPDeliveryCanceledFlush(t *testing.T) {
	t.Parallel()
	for _, async := range []bool{false, true} {
		transport := newTestHTTPDelivery(async, TransportOptions{Dsn: "https://public@example.com/1"}, nil)
		t.Cleanup(transport.Close)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.False(t, transport.FlushWithContext(ctx), "async=%v", async)
	}
}

func TestHTTPDeliveryRequestTimeout(t *testing.T) {
	for _, async := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			deadlineSeen := make(chan time.Duration, 1)
			httpClient := &http.Client{Transport: deliveryRoundTripper(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				if deadline, ok := request.Context().Deadline(); ok {
					deadlineSeen <- time.Until(deadline)
				}
				<-request.Context().Done()
				return nil, request.Context().Err()
			})}
			transport := newTestHTTPDelivery(async, TransportOptions{
				Dsn: "https://public@example.com/1", HTTPClient: httpClient, Timeout: 50 * time.Millisecond,
			}, nil)
			t.Cleanup(transport.Close)
			_ = transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.True(t, transport.FlushWithContext(ctx))
			assert.Equal(t, 50*time.Millisecond, <-deadlineSeen)
			assert.Zero(t, httpClient.Timeout)
		})
	}
}
