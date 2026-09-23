package sentry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"github.com/stretchr/testify/require"
)

func metricEnvelopeWithReport(t *testing.T) *protocol.Envelope {
	t.Helper()
	pending := &report.ClientReport{DiscardedEvents: []report.DiscardedEvent{
		{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 7},
	}}
	item, err := pending.ToEnvelopeItem()
	require.NoError(t, err)
	return protocol.NewEnvelope(&protocol.EnvelopeHeader{},
		protocol.NewTraceMetricItem(3, []byte(`{"items":[{},{},{}]}`)), item)
}

func policyOutcomes(t *testing.T, bodies []string) []report.DiscardedEvent {
	t.Helper()
	var outcomes []report.DiscardedEvent
	for _, body := range bodies {
		lines := strings.Split(strings.TrimSpace(body), "\n")
		for i := 1; i+1 < len(lines); i += 2 {
			var header protocol.EnvelopeItemHeader
			require.NoError(t, json.Unmarshal([]byte(lines[i]), &header))
			require.NotEqual(t, protocol.EnvelopeItemTypeTraceMetric, header.Type, "limited metrics must not reach HTTP")
			if header.Type == protocol.EnvelopeItemTypeClientReport {
				var payload report.ClientReport
				require.NoError(t, json.Unmarshal([]byte(lines[i+1]), &payload))
				outcomes = append(outcomes, payload.DiscardedEvents...)
			}
		}
	}
	return outcomes
}

func TestHTTPDeliveryMetricLimitsPreserveReports(t *testing.T) {
	t.Parallel()
	for _, async := range []bool{false, true} {
		for _, limit := range []string{"1:trace_metric", "1:"} {
			t.Run(fmt.Sprintf("async=%v/limit=%s", async, limit), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
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
							defer mu.Unlock()
							bodies = append(bodies, string(body))
							header := make(http.Header)
							if len(bodies) == 1 {
								header.Set("X-Sentry-Rate-Limits", limit)
							}
							return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}, nil
						}),
					}, report.NewAggregator())
					t.Cleanup(transport.Close)
					require.NoError(t, transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)))
					require.True(t, transport.FlushWithContext(context.Background()))
					require.NoError(t, transport.SendEnvelope(metricEnvelopeWithReport(t)))
					require.True(t, transport.FlushWithContext(context.Background()))
					if limit == "1:" {
						mu.Lock()
						count := len(bodies)
						mu.Unlock()
						require.Equal(t, 1, count, "global backoff must defer the report")
					}
					time.Sleep(2 * time.Second)
					require.True(t, transport.FlushWithContext(context.Background()))
					require.True(t, transport.FlushWithContext(context.Background()))
					mu.Lock()
					defer mu.Unlock()
					require.ElementsMatch(t, []report.DiscardedEvent{
						{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 7},
						{Reason: report.ReasonRateLimitBackoff, Category: ratelimit.CategoryTraceMetric, Quantity: 3},
					}, policyOutcomes(t, bodies))
				})
			})
		}
	}
}

func TestHTTPDeliveryQueuedReports(t *testing.T) {
	t.Parallel()
	for _, rateLimit := range []bool{false, true} {
		t.Run(fmt.Sprintf("rate-limit=%v", rateLimit), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				var bodies []string
				transport := newTestHTTPDelivery(true, TransportOptions{
					Dsn: "https://public@example.com/1", QueueSize: 1,
					HTTPTransport: deliveryRoundTripper(func(request *http.Request) (*http.Response, error) {
						defer request.Body.Close()
						body, err := io.ReadAll(request.Body)
						if err != nil {
							return nil, err
						}
						bodies = append(bodies, string(body))
						header := make(http.Header)
						if len(bodies) == 1 {
							close(started)
							<-release
							if rateLimit {
								header.Set("X-Sentry-Rate-Limits", "1:")
							}
						}
						return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}, nil
					}),
				}, report.NewAggregator())
				t.Cleanup(transport.Close)
				defer unblock()
				require.NoError(t, transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)))
				<-started
				reason := report.ReasonRateLimitBackoff
				if !rateLimit {
					reason = report.ReasonQueueOverflow
					require.NoError(t, transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)))
					require.ErrorIs(t, transport.SendEnvelope(metricEnvelopeWithReport(t)), ErrTransportQueueFull)
				} else {
					require.NoError(t, transport.SendEnvelope(metricEnvelopeWithReport(t)))
				}
				unblock()
				require.True(t, transport.FlushWithContext(context.Background()))
				time.Sleep(2 * time.Second)
				require.True(t, transport.FlushWithContext(context.Background()))
				require.True(t, transport.FlushWithContext(context.Background()))
				require.ElementsMatch(t, []report.DiscardedEvent{
					{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 7},
					{Reason: reason, Category: ratelimit.CategoryTraceMetric, Quantity: 3},
				}, policyOutcomes(t, bodies))
			})
		})
	}
}

type pausedReportProvider struct {
	report.ClientReportProvider
	afterTake func()
}

func (p *pausedReportProvider) TakeReport() *report.ClientReport {
	pending := p.ClientReportProvider.TakeReport()
	if pending != nil {
		p.afterTake()
	}
	return pending
}

func TestHTTPSyncFlushRetainsReportWhenBackoffStarts(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		aggregator := report.NewAggregator()
		aggregator.RecordOne(report.ReasonBeforeSend, ratelimit.CategoryError)
		taken, resume := make(chan struct{}), make(chan struct{})
		unblock := sync.OnceFunc(func() { close(resume) })
		defer unblock()
		provider := &pausedReportProvider{ClientReportProvider: aggregator, afterTake: sync.OnceFunc(func() {
			close(taken)
			<-resume
		})}
		var mu sync.Mutex
		var bodies []string
		transport := newHTTPSyncTransport(TransportOptions{
			Dsn: "https://public@example.com/1",
			HTTPTransport: deliveryRoundTripper(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				mu.Lock()
				defer mu.Unlock()
				bodies = append(bodies, string(body))
				header := make(http.Header)
				if len(bodies) == 1 {
					header.Set("X-Sentry-Rate-Limits", "1:")
				}
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: io.NopCloser(strings.NewReader("{}"))}, nil
			}),
		}, aggregator, provider, nil)
		t.Cleanup(transport.Close)
		flushed := make(chan bool, 1)
		go func() { flushed <- transport.FlushWithContext(context.Background()) }()
		<-taken // Flush has taken the report, but has not checked backoff again.
		require.NoError(t, transport.SendEnvelope(testEnvelope(protocol.EnvelopeItemTypeEvent)))
		unblock()
		require.True(t, <-flushed)
		time.Sleep(2 * time.Second)
		require.True(t, transport.FlushWithContext(context.Background()))
		require.True(t, transport.FlushWithContext(context.Background()))
		mu.Lock()
		defer mu.Unlock()
		require.Len(t, bodies, 2)
		require.Equal(t, []report.DiscardedEvent{
			{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
		}, policyOutcomes(t, bodies))
	})
}
