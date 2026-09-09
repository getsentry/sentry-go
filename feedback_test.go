package sentry

import (
	"encoding/json"
	"testing"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/google/go-cmp/cmp"
)

func TestCaptureFeedback(t *testing.T) {
	client, _, transport := setupClientTest()
	scope := NewScope()
	scope.SetTag("component", "feedback-form")

	eventID := client.CaptureFeedback(&Feedback{
		Message:           "The save button does not work",
		Name:              "Jane Doe",
		Email:             "jane@example.com",
		URL:               "https://example.com/settings",
		Source:            "custom-form",
		AssociatedEventID: "b81c5be4d31e48959103a1f878a1efcb",
	}, nil, scope)

	if eventID == nil {
		t.Fatal("CaptureFeedback returned a nil event ID")
	}

	wantContext := Context{
		"message":             "The save button does not work",
		"name":                "Jane Doe",
		"contact_email":       "jane@example.com",
		"url":                 "https://example.com/settings",
		"source":              "custom-form",
		"associated_event_id": EventID("b81c5be4d31e48959103a1f878a1efcb"),
	}
	if diff := cmp.Diff(wantContext, transport.lastEvent.Contexts[feedbackType]); diff != "" {
		t.Errorf("Feedback context mismatch (-want +got):\n%s", diff)
	}
	if transport.lastEvent.Type != feedbackType {
		t.Errorf("Event type = %q, want %q", transport.lastEvent.Type, feedbackType)
	}
	if transport.lastEvent.Level != LevelInfo {
		t.Errorf("Event level = %q, want %q", transport.lastEvent.Level, LevelInfo)
	}
	if got := transport.lastEvent.Tags["component"]; got != "feedback-form" {
		t.Errorf("Scope tag = %q, want %q", got, "feedback-form")
	}

	item, err := transport.lastEvent.ToEnvelopeItem()
	if err != nil {
		t.Fatal(err)
	}
	if item.Header.Type != protocol.EnvelopeItemTypeFeedback {
		t.Errorf("Envelope item type = %q, want %q", item.Header.Type, protocol.EnvelopeItemTypeFeedback)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["type"]; got != feedbackType {
		t.Errorf("Payload type = %q, want %q", got, feedbackType)
	}
}

func TestCaptureFeedbackOptionalFieldsAreOmitted(t *testing.T) {
	client, scope, transport := setupClientTest()

	client.CaptureFeedback(&Feedback{Message: "It works"}, nil, scope)

	want := Context{"message": "It works"}
	if diff := cmp.Diff(want, transport.lastEvent.Contexts[feedbackType]); diff != "" {
		t.Errorf("Feedback context mismatch (-want +got):\n%s", diff)
	}
}

func TestCaptureFeedbackNil(t *testing.T) {
	client, scope, transport := setupClientTest()

	if eventID := client.CaptureFeedback(nil, nil, scope); eventID != nil {
		t.Errorf("CaptureFeedback(nil) = %q, want nil", *eventID)
	}
	if transport.lastEvent != nil {
		t.Error("CaptureFeedback(nil) sent an event")
	}
}

func TestCaptureFeedbackIgnoresErrorSamplingAndBeforeSend(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.SampleRate = 0
	beforeSendCalled := false
	client.options.BeforeSend = func(_ *Event, _ *EventHint) *Event {
		beforeSendCalled = true
		return nil
	}

	eventID := client.CaptureFeedback(&Feedback{Message: "Feedback"}, nil, scope)

	if eventID == nil {
		t.Fatal("CaptureFeedback returned a nil event ID")
	}
	if beforeSendCalled {
		t.Error("CaptureFeedback called the error BeforeSend hook")
	}
	if transport.lastEvent == nil {
		t.Error("CaptureFeedback did not send an event")
	}
}

func TestEventFromFeedbackMarshalJSONIncludesType(t *testing.T) {
	client, _, _ := setupClientTest()
	event := client.EventFromFeedback(&Feedback{Message: "Feedback"})

	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["type"] != feedbackType {
		t.Errorf("Payload type = %q, want %q", got["type"], feedbackType)
	}
}

func TestHubCaptureFeedbackDoesNotChangeLastEventID(t *testing.T) {
	hub, _, _ := setupHubTest()
	messageID := hub.CaptureMessage("before feedback")

	feedbackID := hub.CaptureFeedback(&Feedback{Message: "Feedback"})

	if feedbackID == nil {
		t.Fatal("CaptureFeedback returned a nil event ID")
	}
	if got := hub.LastEventID(); got != *messageID {
		t.Errorf("LastEventID() = %q, want %q", got, *messageID)
	}
}

func TestHubCaptureFeedbackEventDoesNotChangeLastEventID(t *testing.T) {
	for name, withHint := range map[string]bool{"CaptureEvent": false, "CaptureEventWithHint": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			hub, _, _ := setupHubTest()
			messageID := hub.CaptureMessage("before feedback")
			if messageID == nil {
				t.Fatal("CaptureMessage returned a nil event ID")
			}
			event := NewEvent()
			event.Type = feedbackType
			var feedbackID *EventID
			if withHint {
				feedbackID = hub.CaptureEventWithHint(event, &EventHint{})
			} else {
				feedbackID = hub.CaptureEvent(event)
			}
			if feedbackID == nil {
				t.Fatal("capturing feedback returned a nil event ID")
			}
			if got := hub.LastEventID(); got != *messageID {
				t.Errorf("LastEventID() = %q, want %q", got, *messageID)
			}
		})
	}
}
