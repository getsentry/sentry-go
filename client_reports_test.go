package sentry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/report"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
)

// TestClientReports_Integration tests that client reports are properly generated
// and sent when events are dropped for various reasons.
func TestClientReports_Integration(t *testing.T) {
	var receivedBodies [][]byte
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test-event-id"}`))
	}))
	defer srv.Close()

	dsn := strings.Replace(srv.URL, "//", "//test@", 1) + "/1"
	hub := CurrentHub().Clone()
	c, err := NewClient(ClientOptions{
		Dsn:                  dsn,
		DisableClientReports: false,
		SampleRate:           1.0,
		BeforeSend: func(event *Event, _ *EventHint) *Event {
			if event.Message == "drop-me" {
				return nil
			}
			return event
		},
	})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	hub.BindClient(c)
	defer hub.Flush(testutils.FlushTimeout())

	// second client with disabled reports shouldn't affect the first
	_, _ = NewClient(ClientOptions{
		Dsn:                  testDsn,
		DisableClientReports: true,
	})

	// simulate dropped events for report outcomes
	hub.CaptureMessage("drop-me")
	scope := NewScope()
	scope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		if event.Message == "processor-drop" {
			return nil
		}
		return event
	})
	hub.WithScope(func(s *Scope) {
		s.eventProcessors = scope.eventProcessors
		hub.CaptureMessage("processor-drop")
	})

	hub.CaptureMessage("hi") // send an event to capture the report along with it
	if !hub.Flush(testutils.FlushTimeout()) {
		t.Fatal("Flush timed out")
	}

	var got report.ClientReport
	require.Eventually(t, func() bool {
		mu.Lock()
		bodies := make([][]byte, len(receivedBodies))
		copy(bodies, receivedBodies)
		mu.Unlock()

		for _, b := range bodies {
			for _, line := range bytes.Split(b, []byte("\n")) {
				var report report.ClientReport
				if json.Unmarshal(line, &report) == nil && len(report.DiscardedEvents) > 0 {
					got = report
					return true
				}
			}
		}
		return false
	}, time.Second, 10*time.Millisecond, "no client report found in envelope bodies with: %v", got)

	if got.Timestamp.IsZero() {
		t.Error("client report missing timestamp")
	}

	want := []report.DiscardedEvent{
		{Reason: report.ReasonBeforeSend, Category: ratelimit.CategoryError, Quantity: 1},
		{Reason: report.ReasonEventProcessor, Category: ratelimit.CategoryError, Quantity: 1},
	}
	if diff := cmp.Diff(want, got.DiscardedEvents, cmpopts.SortSlices(func(a, b report.DiscardedEvent) bool {
		return a.Reason < b.Reason
	})); diff != "" {
		t.Errorf("DiscardedEvents mismatch (-want +got):\n%s", diff)
	}
}
