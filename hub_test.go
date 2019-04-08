package sentry

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/stretchr/testify/suite"
)

type HubSuite struct {
	suite.Suite
	client *FakeClient
	scope  *Scope
	hub    *Hub
}

type FakeClient struct {
	options      ClientOptions
	lastCall     string
	lastCallArgs []interface{}
}

func (c *FakeClient) Options() ClientOptions {
	return c.options
}

func (c *FakeClient) CaptureMessage(message string, hint *EventHint, scope ScopeApplier) {
	c.lastCall = "CaptureMessage"
	c.lastCallArgs = []interface{}{message, scope}
}

func (c *FakeClient) CaptureException(exception error, hint *EventHint, scope ScopeApplier) {
	c.lastCall = "CaptureException"
	c.lastCallArgs = []interface{}{exception, scope}
}

func (c *FakeClient) CaptureEvent(event *Event, hint *EventHint, scope ScopeApplier) {
	c.lastCall = "CaptureEvent"
	c.lastCallArgs = []interface{}{event, scope}
}

func (c *FakeClient) Recover(recoveredErr interface{}, scope *Scope) {
	c.lastCall = "Recover"
	c.lastCallArgs = []interface{}{recoveredErr, scope}
}

func (c *FakeClient) RecoverWithContext(ctx context.Context, recoveredErr interface{}, scope *Scope) {
	c.lastCall = "RecoverWithContext"
	c.lastCallArgs = []interface{}{ctx, recoveredErr, scope}
}

func TestHubSuite(t *testing.T) {
	suite.Run(t, new(HubSuite))
}

func (suite *HubSuite) SetupTest() {
	suite.client = &FakeClient{}
	suite.scope = &Scope{}
	suite.hub = NewHub(suite.client, suite.scope)
}

func (suite *HubSuite) TestNewHubPushLayerOnTopOfStack() {
	suite.Len(*suite.hub.stack, 1)
}

func (suite *HubSuite) TestNewHubLayerStoresClientAndScope() {
	suite.Equal(&Layer{client: suite.client, scope: suite.scope}, (*suite.hub.stack)[0])
}

func (suite *HubSuite) TestPushScopeAddsScopeOnTopOfStack() {
	suite.hub.PushScope()

	suite.Len(*suite.hub.stack, 2)
}

func (suite *HubSuite) TestPushScopeInheritsScopeData() {
	suite.scope.SetExtra("foo", "bar")
	suite.hub.PushScope()
	suite.scope.SetExtra("baz", "qux")

	suite.False((*suite.hub.stack)[0].scope == (*suite.hub.stack)[1].scope, "Scope shouldnt point to the same struct")
	suite.Equal(map[string]interface{}{"foo": "bar", "baz": "qux"}, (*suite.hub.stack)[0].scope.extra)
	suite.Equal(map[string]interface{}{"foo": "bar"}, (*suite.hub.stack)[1].scope.extra)
}

func (suite *HubSuite) TestPushScopeInheritsClient() {
	suite.hub.PushScope()

	suite.True((*suite.hub.stack)[0].client == (*suite.hub.stack)[1].client, "Client should be inherited")
}

func (suite *HubSuite) TestPopScopeApplieremovesLayerFromTheStack() {
	suite.hub.PushScope()
	suite.hub.PushScope()
	suite.hub.PopScope()

	suite.Len(*suite.hub.stack, 2)
}

func (suite *HubSuite) TestPopScopeCannotRemoveFromEmptyStack() {
	suite.Len(*suite.hub.stack, 1)
	suite.hub.PopScope()
	suite.Len(*suite.hub.stack, 0)
	suite.hub.PopScope()
	suite.Len(*suite.hub.stack, 0)
}

func (suite *HubSuite) TestBindClient() {
	suite.hub.PushScope()
	newClient := &Client{}
	suite.hub.BindClient(newClient)

	suite.False(
		(*suite.hub.stack)[0].client == (*suite.hub.stack)[1].client,
		"Two stack layers should have different clients bound",
	)
	suite.True((*suite.hub.stack)[0].client == suite.client, "Stack's parent layer should have old client bound")
	suite.True((*suite.hub.stack)[1].client == newClient, "Stack's top layer should have new client bound")
}

func (suite *HubSuite) TestWithScope() {
	suite.hub.WithScope(func(scope *Scope) {
		suite.Len(*suite.hub.stack, 2)
	})

	suite.Len(*suite.hub.stack, 1)
}

func (suite *HubSuite) TestWithScopeBindClient() {
	suite.hub.WithScope(func(scope *Scope) {
		newClient := &Client{}
		suite.hub.BindClient(newClient)
		suite.True(suite.hub.stackTop().client == newClient)
	})

	suite.True(suite.hub.stackTop().client == suite.client)
}

func (suite *HubSuite) TestWithScopeDirectChanges() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.WithScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "bar"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestWithScopeChangesThroughConfigureScope() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.WithScope(func(scope *Scope) {
		suite.hub.ConfigureScope(func(scope *Scope) {
			scope.SetExtra("foo", "baz")
		})
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "bar"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestConfigureScope() {
	suite.hub.Scope().SetExtra("foo", "bar")

	suite.hub.ConfigureScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
	})

	suite.Equal(map[string]interface{}{"foo": "baz"}, suite.hub.stackTop().scope.extra)
}

func (suite *HubSuite) TestLastEventID() {
	uuid := uuid.New()
	hub := &Hub{lastEventID: uuid}
	suite.Equal(uuid, hub.LastEventID())
}

func (suite *HubSuite) TestAccessingEmptyStack() {
	hub := &Hub{}
	suite.Nil(hub.stackTop())
}

func (suite *HubSuite) TestAccessingScopeAppliereturnsNilIfStackIsEmpty() {
	hub := &Hub{}
	suite.Nil(hub.Scope())
}

func (suite *HubSuite) TestAccessingClientReturnsNilIfStackIsEmpty() {
	hub := &Hub{}
	suite.Nil(hub.Client())
}

func (suite *HubSuite) TestAddBreadcrumbRespectMaxBreadcrumbsOption() {
	suite.client.options.MaxBreadcrumbs = 2

	breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

	suite.hub.AddBreadcrumb(breadcrumb, nil)
	suite.hub.AddBreadcrumb(breadcrumb, nil)
	suite.hub.AddBreadcrumb(breadcrumb, nil)

	suite.Len(suite.scope.breadcrumbs, 2)
}

func (suite *HubSuite) TestAddBreadcrumbSkipAllBreadcrumbsIfMaxBreadcrumbsIsLessThanZero() {
	suite.client.options.MaxBreadcrumbs = -1

	breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

	suite.hub.AddBreadcrumb(breadcrumb, nil)
	suite.hub.AddBreadcrumb(breadcrumb, nil)
	suite.hub.AddBreadcrumb(breadcrumb, nil)

	suite.Len(suite.scope.breadcrumbs, 0)
}

func (suite *HubSuite) TestAddBreadcrumbCallsBeforeBreadcrumbCallback() {
	suite.client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
		breadcrumb.Message += "_wat"
		return breadcrumb
	}

	suite.hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

	suite.Len(suite.scope.breadcrumbs, 1)
	suite.Equal("Breadcrumb_wat", suite.scope.breadcrumbs[0].Message)
}

func (suite *HubSuite) TestBeforeBreadcrumbCallbackCanDropABreadcrumb() {
	suite.client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
		return nil
	}

	suite.hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)
	suite.hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

	suite.Len(suite.scope.breadcrumbs, 0)
}

func (suite *HubSuite) TestBeforeBreadcrumbGetAccessToEventHint() {
	suite.client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
		if val, ok := (*hint)["foo"]; ok {
			if val, ok := val.(string); ok {
				breadcrumb.Message += val
			}
		}
		return breadcrumb
	}

	suite.hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, &BreadcrumbHint{"foo": "_oh"})

	suite.Len(suite.scope.breadcrumbs, 1)
	suite.Equal("Breadcrumb_oh", suite.scope.breadcrumbs[0].Message)
}

func (suite *HubSuite) TestInvokeClientExecutesCallbackWithClientAndScopePassed() {
	callback := func(client Clienter, scope *Scope) {
		suite.Equal(suite.client, client)
		suite.Equal(suite.scope, scope)
	}
	suite.hub.invokeClient(callback)
}

func (suite *HubSuite) TestInvokeClientFailsSilentlyWHenNoClientOrScopeAvailable() {
	hub := &Hub{}
	callback := func(_ Clienter, _ *Scope) {
		suite.Fail("callback shoudnt be executed")
	}
	suite.NotPanics(func() {
		hub.invokeClient(callback)
	})
}

func (suite *HubSuite) TestCaptureEventCallsTheSameMethodOnClient() {
	event := &Event{Message: "CaptureEvent"}

	suite.hub.CaptureEvent(event, nil)

	suite.Equal("CaptureEvent", suite.client.lastCall)
	suite.Equal(event, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestCaptureMessageCallsTheSameMethodOnClient() {
	suite.hub.CaptureMessage("foo", nil)

	suite.Equal("CaptureMessage", suite.client.lastCall)
	suite.Equal("foo", suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestCaptureExceptionCallsTheSameMethodOnClient() {
	err := errors.New("error")

	suite.hub.CaptureException(err, nil)

	suite.Equal("CaptureException", suite.client.lastCall)
	suite.Equal(err, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestRecoverCallsTheSameMethodOnClient() {
	err := errors.New("error")

	suite.hub.Recover(err)

	suite.Equal("Recover", suite.client.lastCall)
	suite.Equal(err, suite.client.lastCallArgs[0])
	suite.Equal(suite.scope, suite.client.lastCallArgs[1])
}

func (suite *HubSuite) TestRecoverWithContextCallsTheSameMethodOnClient() {
	ctx := context.TODO()
	err := errors.New("error")

	suite.hub.RecoverWithContext(ctx, err)

	suite.Equal("RecoverWithContext", suite.client.lastCall)
	suite.Equal(ctx, suite.client.lastCallArgs[0])
	suite.Equal(err, suite.client.lastCallArgs[1])
	suite.Equal(suite.scope, suite.client.lastCallArgs[2])
}

func (suite *HubSuite) TestFlushShouldPanicTillImplemented() {
	suite.Panics(func() {
		suite.hub.Flush(0)
	})
}

func (suite *HubSuite) TestHasHubOnContextReturnsTrueIfHubIsThere() {
	ctx := context.Background()

	ctx = SetHubOnContext(ctx, suite.hub)

	suite.True(HasHubOnContext(ctx))
}

func (suite *HubSuite) TestHasHubOnContextReturnsFalseIfHubIsNotThere() {
	ctx := context.Background()

	suite.False(HasHubOnContext(ctx))
}

func (suite *HubSuite) TestGetHubFromContext() {
	ctx := context.Background()

	ctx = SetHubOnContext(ctx, suite.hub)
	hub := GetHubFromContext(ctx)

	suite.Equal(hub, suite.hub)
}

func (suite *HubSuite) TestGetHubFromContextReturnsNilIfHubIsNotThere() {
	ctx := context.Background()

	hub := GetHubFromContext(ctx)

	suite.Nil(hub)
}

func (suite *HubSuite) TestSetHubOnContextReturnsNewContext() {
	ctx := context.Background()

	ctxWithHub := SetHubOnContext(ctx, suite.hub)

	suite.NotEqual(suite.hub, ctxWithHub)
}
