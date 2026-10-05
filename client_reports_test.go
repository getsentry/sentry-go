package sentry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"github.com/stretchr/testify/require"
)

type reportingTransport struct {
	NoopTransport
	err error
}

func (t *reportingTransport) SendEnvelope(_ *protocol.Envelope) error { return t.err }

func TestClientReports_CustomTransport(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		err      error
		reason   report.DiscardReason
		disabled bool
	}{
		{"queue rejection", ErrTransportQueueFull, report.ReasonQueueOverflow, false},
		{"send rejection", errors.New("rejected"), report.ReasonSendError, false},
		{"disabled", ErrTransportQueueFull, report.ReasonQueueOverflow, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				DisableClientReports: tt.disabled,
				Transport:            &reportingTransport{err: tt.err},
				BeforeSend: func(event *Event, _ *EventHint) *Event {
					if event.Message == "drop" {
						return nil
					}
					return event
				},
			})
			require.NoError(t, err)
			defer client.Close()
			ctx, _ := WithScope(context.Background())
			client.CaptureMessage(ctx, "drop")
			id := client.CaptureMessage(ctx, "send")
			require.Equal(t, tt.err == nil, id != nil)
			pending := client.reportProvider.TakeReport()
			if tt.disabled {
				require.Nil(t, pending)
				return
			}
			require.NotNil(t, pending)
			require.ElementsMatch(t, []report.DiscardedEvent{
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
				{Reason: tt.reason, Category: ratelimit.CategoryError, Quantity: 1},
			}, pending.DiscardedEvents)
		})
	}
}

// TestClientReports_Integration tests that client reports are properly generated
// and sent when events are dropped for various reasons.
func TestClientReports_Integration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		reportOnly bool
		disabled   bool
		status     int
		transport  Transport
	}{
		{"attached", false, false, http.StatusOK, nil},
		{"report-only flush", true, false, http.StatusOK, nil},
		{"disabled", true, true, http.StatusOK, nil},
		{"failed report is retained without retrying", true, false, http.StatusInternalServerError, nil},
		{"wrapped attached", false, false, http.StatusOK, &wrappedTransport{NewSyncTransport()}},
		{"wrapped disabled", true, true, http.StatusOK, &wrappedTransport{NewSyncTransport()}},
		{"wrapped failed report", true, false, http.StatusInternalServerError, &wrappedTransport{NewSyncTransport()}},
		{"wrapped failed event", false, false, http.StatusInternalServerError, &wrappedTransport{NewSyncTransport()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBodies [][]byte
			var mu sync.Mutex
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				mu.Lock()
				receivedBodies = append(receivedBodies, body)
				mu.Unlock()
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(`{"id":"test-event-id"}`))
			}))
			defer srv.Close()

			dsn := strings.Replace(srv.URL, "//", "//test@", 1) + "/1"
			c, err := NewClient(ClientOptions{
				Dsn:                  dsn,
				Transport:            tt.transport,
				DisableClientReports: tt.disabled,
				SampleRate:           1.0,
				BeforeSend: func(event *Event, _ *EventHint) *Event {
					if event.Message == "drop-me" {
						return nil
					}
					return event
				},
			})
			require.NoError(t, err)
			defer c.Close()

			// A second client's disabled reports must not affect the first client.
			disabled, err := NewClient(ClientOptions{Dsn: testDsn, DisableClientReports: true})
			require.NoError(t, err)
			defer disabled.Close()

			ctx, _ := WithScope(context.Background())
			c.CaptureMessage(ctx, "drop-me")
			processorCtx, processorScope := WithScope(ctx)
			processorScope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
				if event.Message == "processor-drop" {
					return nil
				}
				return event
			})
			c.CaptureMessage(processorCtx, "processor-drop")
			if !tt.reportOnly {
				c.CaptureMessage(ctx, "hi")
			}
			require.True(t, c.Flush(testutils.FlushTimeout()))

			if tt.disabled {
				mu.Lock()
				count := len(receivedBodies)
				mu.Unlock()
				require.Zero(t, count)
				require.Nil(t, c.reportProvider.TakeReport())
				return
			}
			var got report.ClientReport
			require.Eventually(t, func() bool {
				mu.Lock()
				defer mu.Unlock()
				for _, body := range receivedBodies {
					for _, line := range bytes.Split(body, []byte("\n")) {
						var candidate report.ClientReport
						if json.Unmarshal(line, &candidate) == nil && len(candidate.DiscardedEvents) > 0 {
							got = candidate
							return true
						}
					}
				}
				return false
			}, time.Second, 10*time.Millisecond, "no client report received")
			if tt.reportOnly {
				mu.Lock()
				count := len(receivedBodies)
				mu.Unlock()
				require.Equal(t, 1, count, "flush must not retry failed reports")
			}
			pending := c.reportProvider.TakeReport()
			require.False(t, got.Timestamp.IsZero(), "client report missing timestamp")
			want := []report.DiscardedEvent{
				{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
				{Reason: report.ReasonEventProcessor, Category: ratelimit.CategoryError, Quantity: 1},
			}
			require.ElementsMatch(t, want, got.DiscardedEvents)
			if tt.status == http.StatusOK {
				require.Nil(t, pending)
			} else {
				require.NotNil(t, pending)
				if !tt.reportOnly {
					want = append(want, report.DiscardedEvent{Reason: report.ReasonSendError, Category: ratelimit.CategoryError, Quantity: 1})
				}
				require.ElementsMatch(t, want, pending.DiscardedEvents)
			}
		})
	}
}
