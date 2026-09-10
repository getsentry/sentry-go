package sentry

import (
	"encoding/json"
	"testing"

	"github.com/getsentry/sentry-go/internal/testutils"
)

func jsonContext(t testing.TB, value Context) Context {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Context
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func capturedEvents(t testing.TB, client *Client, transport *MockTransport) []*Event {
	t.Helper()
	if !client.Flush(testutils.FlushTimeout()) {
		t.Fatal("client flush timed out")
	}
	return transport.Events()
}

func lastCapturedEvent(t testing.TB, client *Client, transport *MockTransport) *Event {
	t.Helper()
	events := capturedEvents(t, client, transport)
	if len(events) == 0 {
		return nil
	}
	return events[len(events)-1]
}

func capturedEventOfType(t testing.TB, client *Client, transport *MockTransport, kind string) *Event {
	t.Helper()
	for _, event := range capturedEvents(t, client, transport) {
		if event.Type == kind {
			return event
		}
	}
	t.Fatalf("missing event type %q", kind)
	return nil
}

func capturedTrace(t testing.TB, transport *MockTransport, event *Event) map[string]string {
	t.Helper()
	for _, envelope := range transport.Envelopes() {
		if envelope.Header.EventID == string(event.EventID) {
			if len(envelope.Header.Trace) == 0 {
				return nil
			}
			return envelope.Header.Trace
		}
	}
	t.Fatalf("missing envelope for event %s", event.EventID)
	return nil
}
