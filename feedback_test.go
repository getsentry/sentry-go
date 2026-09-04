package sentry

import (
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
