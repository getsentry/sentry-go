package sentry

import (
	"errors"
	"testing"

	pkgErrors "github.com/pkg/errors"
)

func TestNewClientAllowsEmptyDSN(t *testing.T) {
	transport := &TransportMock{}
	client, err := NewClient(ClientOptions{
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("expected no error when creating client without a DNS but got %v", err)
	}

	client.CaptureException(errors.New("custom error"), nil, &ScopeMock{})
	assertEqual(t, transport.lastEvent.Exception[0].Value, "custom error")
}

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
	client, _ := NewClient(ClientOptions{
		Dsn:       "http://whatever@really.com/1337",
		Transport: transport,
		Integrations: func(i []Integration) []Integration {
			return []Integration{}
		},
	})

	return client, scope, transport
}
func TestCaptureMessageShouldSendEventWithProvidedMessage(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureMessage("foo", nil, scope)
	assertEqual(t, transport.lastEvent.Message, "foo")
}

func TestCaptureMessageShouldSucceedWithoutNilScope(t *testing.T) {
	client, _, transport := setupClientTest()
	client.CaptureMessage("foo", nil, nil)
	assertEqual(t, transport.lastEvent.Message, "foo")
}

func TestCaptureExceptionShouldSendEventWithProvidedError(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureException(errors.New("custom error"), nil, scope)
	assertEqual(t, transport.lastEvent.Exception[0].Type, "*errors.errorString")
	assertEqual(t, transport.lastEvent.Exception[0].Value, "custom error")
}

func TestCaptureExceptionShouldNotFailWhenPassedNil(t *testing.T) {
	client, scope, transport := setupClientTest()
	client.CaptureException(nil, nil, scope)
	assertEqual(t, transport.lastEvent.Message, "Called CaptureException with nil value")
}

type customErr struct{}

func (e *customErr) Error() string {
	return "wat"
}

func TestCaptureExceptionShouldExtractCorrectTypeAndValueForWrappedErrors(t *testing.T) {
	client, scope, transport := setupClientTest()
	cause := &customErr{}
	err := pkgErrors.WithStack(cause)
	client.CaptureException(err, nil, scope)
	assertEqual(t, transport.lastEvent.Exception[0].Type, "*sentry.customErr")
	assertEqual(t, transport.lastEvent.Exception[0].Value, "wat")
}

type customErrWithCause struct{ cause error }

func (e *customErrWithCause) Error() string {
	return "err"
}

func (e *customErrWithCause) Cause() error {
	return e.cause
}

func TestCaptureExceptionShouldNotUseCauseIfCauseIsNil(t *testing.T) {
	client, scope, transport := setupClientTest()
	err := &customErrWithCause{cause: nil}
	client.CaptureException(err, nil, scope)
	assertEqual(t, transport.lastEvent.Exception[0].Type, "*sentry.customErrWithCause")
	assertEqual(t, transport.lastEvent.Exception[0].Value, "err")
}

func TestCaptureExceptionShouldUseCauseIfCauseIsNotNil(t *testing.T) {
	client, scope, transport := setupClientTest()
	err := &customErrWithCause{cause: &customErr{}}
	client.CaptureException(err, nil, scope)
	assertEqual(t, transport.lastEvent.Exception[0].Type, "*sentry.customErr")
	assertEqual(t, transport.lastEvent.Exception[0].Value, "wat")
}

func TestCaptureEventShouldSendEventWithProvidedError(t *testing.T) {
	client, scope, transport := setupClientTest()
	event := NewEvent()
	event.Message = "event message"
	client.CaptureEvent(event, nil, scope)
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
