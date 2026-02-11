package sentry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/report"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestClientReports_Integration tests that client reports are properly generated
// and sent when events are dropped for various reasons.
func TestClientReports_Integration(t *testing.T) {
	var receivedBodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, body)
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
	found := false
	for _, b := range receivedBodies {
		for _, line := range bytes.Split(b, []byte("\n")) {
			if json.Unmarshal(line, &got) == nil && len(got.DiscardedEvents) > 0 {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("no client report found in envelope bodies")
	}

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
