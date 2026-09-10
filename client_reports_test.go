package sentry

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
	"github.com/getsentry/sentry-go/report"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
)

// TestClientReports_Integration tests that client reports are properly generated
// and sent when events are dropped for various reasons.
func TestClientReports_Integration(t *testing.T) {
	for _, test := range []struct {
		name         string
		newTransport func(TransportOptions) Transport
	}{
		{"default", nil}, {"wrapped async", NewHTTPTransport}, {"wrapped sync", NewHTTPSyncTransport},
	} {
		t.Run(test.name, func(t *testing.T) { testClientReports(t, test.newTransport, false) })
		t.Run(test.name+"/disabled", func(t *testing.T) { testClientReports(t, test.newTransport, true) })
	}
}

type reportTestWrapper struct{ Transport }

func testClientReports(t *testing.T, newTransport func(TransportOptions) Transport, disabled bool) {
	t.Helper()
	var receivedBodies [][]byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, body)
		mu.Unlock()
		if bytes.Contains(body, []byte("send-error")) {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test-event-id"}`))
	}))
	defer srv.Close()

	dsn := strings.Replace(srv.URL, "//", "//test@", 1) + "/1"
	options := ClientOptions{
		Dsn:                  dsn,
		DisableClientReports: disabled,
		SampleRate:           1.0,
		BeforeSend: func(event *Event, _ *EventHint) *Event {
			if event.Message == "drop-me" {
				return nil
			}
			return event
		},
	}
	if newTransport != nil {
		options.Transport = &reportTestWrapper{newTransport(TransportOptions{
			Dsn: dsn, DisableClientReports: disabled,
		})}
	}
	c, err := NewClient(options)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	ctx, _ := WithIsolationScope(context.Background())
	t.Cleanup(c.Close)

	// second client with disabled reports shouldn't affect the first
	other, err := NewClient(ClientOptions{
		Dsn:                  testDsn,
		DisableClientReports: true,
	})

	require.NoError(t, err)
	t.Cleanup(other.Close)
	// simulate dropped events for report outcomes
	c.CaptureMessage(ctx, "drop-me")
	processorCtx, processorScope := WithIsolationScope(ctx)
	processorScope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		if event.Message == "processor-drop" {
			return nil
		}
		return event
	})
	c.CaptureMessage(processorCtx, "processor-drop")

	c.CaptureMessage(ctx, "hi") // send an event to capture the report along with it
	if !c.Flush(testutils.FlushTimeout()) {
		t.Fatal("Flush timed out")
	}

	c.CaptureMessage(ctx, "send-error")
	require.True(t, c.Flush(testutils.FlushTimeout()))
	var got report.ClientReport
	mu.Lock()
	for _, body := range receivedBodies {
		for _, line := range bytes.Split(body, []byte("\n")) {
			var payload report.ClientReport
			if json.Unmarshal(line, &payload) == nil && len(payload.DiscardedEvents) > 0 {
				got.Timestamp = payload.Timestamp
				got.DiscardedEvents = append(got.DiscardedEvents, payload.DiscardedEvents...)
			}
		}
	}
	mu.Unlock()
	if disabled {
		require.Empty(t, got.DiscardedEvents)
		return
	}

	if got.Timestamp.IsZero() {
		t.Error("client report missing timestamp")
	}

	want := []report.DiscardedEvent{
		{Reason: report.ReasonBeforeSend, Category: protocol.CategoryError, Quantity: 1},
		{Reason: report.ReasonEventProcessor, Category: protocol.CategoryError, Quantity: 1},
		{Reason: report.ReasonSendError, Category: protocol.CategoryError, Quantity: 1},
	}
	if diff := cmp.Diff(want, got.DiscardedEvents, cmpopts.SortSlices(func(a, b report.DiscardedEvent) bool {
		return a.Reason < b.Reason
	})); diff != "" {
		t.Errorf("DiscardedEvents mismatch (-want +got):\n%s", diff)
	}
}
