package sentry

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ClientSuite struct {
	suite.Suite
	scope  *FakeScope
	client *Client
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

func (suite *ClientSuite) SetupTest() {
	suite.scope = &FakeScope{}
	suite.client = &Client{}
}

func TestClientSuite(t *testing.T) {
	suite.Run(t, new(ClientSuite))
}

func (suite *ClientSuite) TestAddBreadcrumbCallsTheSameMethodOnScope() {
	breadcrumb := &Breadcrumb{Message: "foo"}
	suite.client.AddBreadcrumb(breadcrumb, suite.scope)
	suite.Equal(suite.scope.breadcrumb, breadcrumb)
}
