package sentry

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ClientSuite struct {
	suite.Suite
	scope     *FakeScope
	transport *FakeTransport
	client    *Client
}

type FakeScope struct {
	breadcrumb      *Breadcrumb
	shouldDropEvent bool
}

func (scope *FakeScope) AddBreadcrumb(breadcrumb *Breadcrumb) {
	scope.breadcrumb = breadcrumb
}

func (scope *FakeScope) ApplyToEvent(event *Event) *Event {
	if scope.shouldDropEvent {
		return nil
	}
	return event
}

type FakeTransport struct {
	lastEvent *Event
}

func (t *FakeTransport) Configure(options ClientOptions) {}
func (t *FakeTransport) SendEvent(event *Event) (*http.Response, error) {
	t.lastEvent = event
	return nil, nil
}

func (suite *ClientSuite) SetupTest() {
	suite.scope = &FakeScope{}
	suite.transport = &FakeTransport{}
	suite.client = &Client{
		Transport: suite.transport,
	}
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (suite *ClientSuite) TestAddBreadcrumbCallsTheSameMethodOnScope() {
	breadcrumb := &Breadcrumb{Message: "foo"}
	suite.client.AddBreadcrumb(breadcrumb, nil, suite.scope)
	suite.Equal(suite.scope.breadcrumb, breadcrumb)
}

func (suite *ClientSuite) TestCaptureMessageShouldSendEventWithProvidedMessage() {
	suite.client.CaptureMessage("foo", nil, suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "foo")
}

func (suite *ClientSuite) TestCaptureExceptionShouldSendEventWithProvidedError() {
	suite.client.CaptureException(errors.New("custom error"), nil, suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "custom error")
}

func (suite *ClientSuite) TestCaptureEventShouldSendEventWithProvidedError() {
	suite.client.CaptureEvent(&Event{Message: "event message"}, nil, suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "event message")
}

func (suite *ClientSuite) TestSampleRateCanDropEvent() {
	suite.client.Options.SampleRate = 0.000000000000001
	suite.client.CaptureMessage("Foo", nil, suite.scope)
	suite.Nil(suite.transport.lastEvent)
}

func (suite *ClientSuite) TestApplyToScopeCanDropEvent() {
	suite.scope.shouldDropEvent = true
	suite.client.CaptureMessage("Foo", nil, suite.scope)
	suite.Nil(suite.transport.lastEvent)
}

func (suite *ClientSuite) TestBeforeSendCanDropEvent() {
	suite.client.Options.BeforeSend = func(event *Event, hint *EventHint) *Event {
		return nil
	}
	suite.client.CaptureMessage("Foo", nil, suite.scope)
	suite.Nil(suite.transport.lastEvent)
}

type CustomComplexError struct {
	Message string
}

func (e CustomComplexError) Error() string {
	return "CustomComplexError: " + e.Message
}

func (e CustomComplexError) AnswerToLife() string {
	return "42"
}

func (suite *ClientSuite) TestBeforeSendGetAccessToEventHint() {
	suite.client.Options.BeforeSend = func(event *Event, hint *EventHint) *Event {
		if ex, ok := hint.OriginalException.(CustomComplexError); ok {
			event.Message = event.Message + " " + ex.AnswerToLife()
		}
		return event
	}
	ex := CustomComplexError{Message: "Foo"}
	suite.client.CaptureException(ex, &EventHint{OriginalException: ex}, suite.scope)
	suite.Equal("CustomComplexError: Foo 42", suite.transport.lastEvent.Message)
}
