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
	suite.client.AddBreadcrumb(breadcrumb, suite.scope)
	suite.Equal(suite.scope.breadcrumb, breadcrumb)
}

func (suite *ClientSuite) TestCaptureMessageShouldSendEventWithProvidedMessage() {
	suite.client.CaptureMessage("foo", suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "foo")
}

func (suite *ClientSuite) TestCaptureExceptionShouldSendEventWithProvidedError() {
	suite.client.CaptureException(errors.New("custom error"), suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "custom error")
}

func (suite *ClientSuite) TestCaptureEventShouldSendEventWithProvidedError() {
	suite.client.CaptureEvent(&Event{Message: "event message"}, suite.scope)
	suite.Equal(suite.transport.lastEvent.Message, "event message")
}

func (suite *ClientSuite) TestSampleRateCanDropEvent() {
	suite.client.Options.SampleRate = 0.000000000000001
	suite.client.CaptureMessage("Foo", suite.scope)
	suite.Nil(suite.transport.lastEvent)
}

func (suite *ClientSuite) TestApplyToScopeCanDropEvent() {
	suite.scope.shouldDropEvent = true
	suite.client.CaptureMessage("Foo", suite.scope)
	suite.Nil(suite.transport.lastEvent)
}

func (suite *ClientSuite) TestBeforeSendCanDropEvent() {
	suite.client.Options.BeforeSend = func(event *Event) *Event {
		return nil
	}
	suite.client.CaptureMessage("Foo", suite.scope)
	suite.Nil(suite.transport.lastEvent)
}
