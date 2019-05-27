package sentry

import (
	"errors"
	"testing"
)

type customComplexError struct {
	Message string
}

func (e customComplexError) Error() string {
	return "customComplexError: " + e.Message
}

func (e customComplexError) AnswerToLife() string {
	return "42"
}

func setupClientTest() (*Client, *ScopeMock, *TransportMock) {
	scope := &ScopeMock{}
	transport := &TransportMock{}
	client := &Client{
		Transport: transport,
	}

	return client, scope, transport
}
func TestCaptureMessageShouldSendEventWithProvidedMessage(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureMessage("foo", nil, scope)
	assertEqual(t, transport.lastEvent.Message, "foo")
}

func TestCaptureExceptionShouldSendEventWithProvidedError(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureException(errors.New("custom error"), nil, scope)
	assertEqual(t, transport.lastEvent.Exception[0].Value, "custom error")
}

func TestCaptureExceptionShouldNotFailWhenPassedNil(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureException(nil, nil, scope)
	assertEqual(t, transport.lastEvent.Message, "Called CaptureException with nil value")
}

func TestCaptureEventShouldSendEventWithProvidedError(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureEvent(&Event{Message: "event message"}, nil, scope)
	assertEqual(t, transport.lastEvent.Message, "event message")
}

func TestSampleRateCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.SampleRate = 0.000000000000001

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestApplyToScopeCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	scope.shouldDropEvent = true

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestBeforeSendCanDropEvent(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.BeforeSend = func(event *Event, hint *EventHint) *Event {
		return nil
	}

	client.CaptureMessage("Foo", nil, scope)

	if transport.lastEvent != nil {
		t.Error("expected event to be dropped")
	}
}

func TestBeforeSendGetAccessToEventHint(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.options.BeforeSend = func(event *Event, hint *EventHint) *Event {
		if ex, ok := hint.OriginalException.(customComplexError); ok {
			event.Message = event.Exception[0].Value + " " + ex.AnswerToLife()
		}
		return event
	}
	ex := customComplexError{Message: "Foo"}

	client.CaptureException(ex, &EventHint{OriginalException: ex}, scope)

	assertEqual(t, transport.lastEvent.Message, "customComplexError: Foo 42")
}
