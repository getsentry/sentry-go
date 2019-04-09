package sentry

import (
	"context"
	"errors"
	"testing"
)

type ClientMock struct {
	options      ClientOptions
	lastCall     string
	lastCallArgs []interface{}
}

func (c *ClientMock) Options() ClientOptions {
	return c.options
}

func (c *ClientMock) CaptureMessage(message string, hint *EventHint, scope EventModifier) {
	c.lastCall = "CaptureMessage"
	c.lastCallArgs = []interface{}{message, scope}
}

func (c *ClientMock) CaptureException(exception error, hint *EventHint, scope EventModifier) {
	c.lastCall = "CaptureException"
	c.lastCallArgs = []interface{}{exception, scope}
}

func (c *ClientMock) CaptureEvent(event *Event, hint *EventHint, scope EventModifier) {
	c.lastCall = "CaptureEvent"
	c.lastCallArgs = []interface{}{event, scope}
}

func (c *ClientMock) Recover(recoveredErr interface{}, scope *Scope) {
	c.lastCall = "Recover"
	c.lastCallArgs = []interface{}{recoveredErr, scope}
}

func (c *ClientMock) RecoverWithContext(ctx context.Context, recoveredErr interface{}, scope *Scope) {
	c.lastCall = "RecoverWithContext"
	c.lastCallArgs = []interface{}{ctx, recoveredErr, scope}
}

func setupHubTest() (*Hub, *ClientMock, *Scope) {
	client := &ClientMock{}
	scope := &Scope{}
	hub := NewHub(client, scope)

	return hub, client, scope
}

func TestNewHub(t *testing.T) {
	t.Run("PushesLayerOnTopOfStack", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		assertEqual(t, len(*hub.stack), 1)
	})

	t.Run("LayerStoresClientAndScope", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		assertEqual(t, &Layer{client: client, scope: scope}, (*hub.stack)[0])
	})
}

func TestPushScope(t *testing.T) {
	t.Run("AddsScopeOnTopOfStack", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		hub.PushScope()
		assertEqual(t, len(*hub.stack), 2)
	})

	t.Run("InheritsScopeData", func(t *testing.T) {
		hub, _, scope := setupHubTest()
		scope.SetExtra("foo", "bar")
		hub.PushScope()
		scope.SetExtra("baz", "qux")

		if (*hub.stack)[0].scope == (*hub.stack)[1].scope {
			t.Error("Scope shouldnt point to the same struct")
		}
		assertEqual(t, map[string]interface{}{"foo": "bar", "baz": "qux"}, (*hub.stack)[0].scope.extra)
		assertEqual(t, map[string]interface{}{"foo": "bar"}, (*hub.stack)[1].scope.extra)
	})

	t.Run("InheritsClient", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		hub.PushScope()

		if (*hub.stack)[0].client != (*hub.stack)[1].client {
			t.Error("Client should be inherited")
		}
	})
}

func TestPopScope(t *testing.T) {
	t.Run("RemovesLayerFromTheStack", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		hub.PushScope()
		hub.PushScope()
		hub.PopScope()

		assertEqual(t, len(*hub.stack), 2)
	})

	t.Run("CannotRemoveFromEmptyStack", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		assertEqual(t, len(*hub.stack), 1)
		hub.PopScope()
		assertEqual(t, len(*hub.stack), 0)
		hub.PopScope()
		assertEqual(t, len(*hub.stack), 0)
	})
}

func TestBindClient(t *testing.T) {
	hub, client, _ := setupHubTest()
	hub.PushScope()
	newClient := &ClientMock{}
	hub.BindClient(newClient)

	if (*hub.stack)[0].client == (*hub.stack)[1].client {
		t.Error("Two stack layers should have different clients bound")
	}
	if (*hub.stack)[0].client != client {
		t.Error("Stack's parent layer should have old client bound")
	}
	if (*hub.stack)[1].client != newClient {
		t.Error("Stack's top layer should have new client bound")
	}
}

func TestWithScope(t *testing.T) {
	t.Run("CreatesIsolatedScope", func(t *testing.T) {
		hub, _, _ := setupHubTest()

		hub.WithScope(func(scope *Scope) {
			assertEqual(t, len(*hub.stack), 2)
		})

		assertEqual(t, len(*hub.stack), 1)
	})

	t.Run("BindClient", func(t *testing.T) {
		hub, client, _ := setupHubTest()

		hub.WithScope(func(scope *Scope) {
			newClient := &ClientMock{}
			hub.BindClient(newClient)
			if hub.stackTop().client != newClient {
				t.Error("should use newly bound client")
			}
		})

		if hub.stackTop().client != client {
			t.Error("should use old client")
		}
	})

	t.Run("DirectChanges", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		hub.Scope().SetExtra("foo", "bar")

		hub.WithScope(func(scope *Scope) {
			scope.SetExtra("foo", "baz")
			assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
		})

		assertEqual(t, map[string]interface{}{"foo": "bar"}, hub.stackTop().scope.extra)
	})

	t.Run("ChangesThroughConfigureScope", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		hub.Scope().SetExtra("foo", "bar")

		hub.WithScope(func(scope *Scope) {
			hub.ConfigureScope(func(scope *Scope) {
				scope.SetExtra("foo", "baz")
			})
			assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
		})

		assertEqual(t, map[string]interface{}{"foo": "bar"}, hub.stackTop().scope.extra)
	})
}

func TestConfigureScope(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.Scope().SetExtra("foo", "bar")

	hub.ConfigureScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
	})

	assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
}

func TestLastEventID(t *testing.T) {
	uuid := uuid()
	hub := &Hub{lastEventID: uuid}
	assertEqual(t, uuid, hub.LastEventID())
}

func TestLayerAccess(t *testing.T) {
	t.Run("AccessingEmptyStack", func(t *testing.T) {
		hub := &Hub{}
		if hub.stackTop() != nil {
			t.Error("expected nil to be returned")
		}
	})

	t.Run("AccessingScopeReturnsNilIfStackIsEmpty", func(t *testing.T) {
		hub := &Hub{}
		if hub.Scope() != nil {
			t.Error("expected nil to be returned")
		}
	})

	t.Run("AccessingClientReturnsNilIfStackIsEmpty", func(t *testing.T) {
		hub := &Hub{}
		if hub.Client() != nil {
			t.Error("expected nil to be returned")
		}
	})
}

func TestHubsAddBreadcrumb(t *testing.T) {
	t.Run("RespectMaxBreadcrumbsOption", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		client.options.MaxBreadcrumbs = 2

		breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

		hub.AddBreadcrumb(breadcrumb, nil)
		hub.AddBreadcrumb(breadcrumb, nil)
		hub.AddBreadcrumb(breadcrumb, nil)

		assertEqual(t, len(scope.breadcrumbs), 2)
	})

	t.Run("SkipAllBreadcrumbsIfMaxBreadcrumbsIsLessThanZero", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		client.options.MaxBreadcrumbs = -1

		breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

		hub.AddBreadcrumb(breadcrumb, nil)
		hub.AddBreadcrumb(breadcrumb, nil)
		hub.AddBreadcrumb(breadcrumb, nil)

		assertEqual(t, len(scope.breadcrumbs), 0)
	})

	t.Run("CallsBeforeBreadcrumbCallback", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
			breadcrumb.Message += "_wat"
			return breadcrumb
		}

		hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

		assertEqual(t, len(scope.breadcrumbs), 1)
		assertEqual(t, "Breadcrumb_wat", scope.breadcrumbs[0].Message)
	})
}
func TestBeforeBreadcrumb(t *testing.T) {
	t.Run("CallbackCanDropABreadcrumb", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
			return nil
		}

		hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)
		hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

		assertEqual(t, len(scope.breadcrumbs), 0)
	})

	t.Run("GetAccessToEventHint", func(t *testing.T) {
		hub, client, scope := setupHubTest()
		client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
			if val, ok := (*hint)["foo"]; ok {
				if val, ok := val.(string); ok {
					breadcrumb.Message += val
				}
			}

			return breadcrumb
		}

		hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, &BreadcrumbHint{"foo": "_oh"})

		assertEqual(t, len(scope.breadcrumbs), 1)
		assertEqual(t, "Breadcrumb_oh", scope.breadcrumbs[0].Message)
	})
}

func TestInvokeClient(t *testing.T) {
	t.Run("ExecutesCallbackWithClientAndScopePassed", func(t *testing.T) {
		hub, _, _ := setupHubTest()
		callback := func(client Clienter, scope *Scope) {
			assertEqual(t, client, client)
			assertEqual(t, scope, scope)
		}
		hub.invokeClient(callback)
	})

	t.Run("FailsSilentlyWHenNoClientOrScopeAvailable", func(t *testing.T) {
		hub := &Hub{}
		callback := func(_ Clienter, _ *Scope) {
			t.Error("callback shoudnt be executed")
		}

		func() {
			defer func() {
				err := recover()
				if err != nil {
					t.Error("invokeClient should not panic")
				}
			}()

			hub.invokeClient(callback)
		}()
	})
}

func TestCaptureEventCallsTheSameMethodOnClient(t *testing.T) {
	hub, client, scope := setupHubTest()

	event := &Event{Message: "CaptureEvent"}

	hub.CaptureEvent(event, nil)

	assertEqual(t, "CaptureEvent", client.lastCall)
	assertEqual(t, event, client.lastCallArgs[0])
	assertEqual(t, scope, client.lastCallArgs[1])
}

func TestCaptureMessageCallsTheSameMethodOnClient(t *testing.T) {
	hub, client, scope := setupHubTest()

	hub.CaptureMessage("foo", nil)

	assertEqual(t, "CaptureMessage", client.lastCall)
	assertEqual(t, "foo", client.lastCallArgs[0])
	assertEqual(t, scope, client.lastCallArgs[1])
}

func TestCaptureExceptionCallsTheSameMethodOnClient(t *testing.T) {
	hub, client, scope := setupHubTest()

	err := errors.New("error")

	hub.CaptureException(err, nil)

	assertEqual(t, "CaptureException", client.lastCall)
	assertEqual(t, err, client.lastCallArgs[0])
	assertEqual(t, scope, client.lastCallArgs[1])
}

func TestRecoverCallsTheSameMethodOnClient(t *testing.T) {
	hub, client, scope := setupHubTest()

	err := errors.New("error")

	hub.Recover(err)

	assertEqual(t, "Recover", client.lastCall)
	assertEqual(t, err, client.lastCallArgs[0])
	assertEqual(t, scope, client.lastCallArgs[1])
}

func TestRecoverWithContextCallsTheSameMethodOnClient(t *testing.T) {
	hub, client, scope := setupHubTest()

	ctx := context.TODO()
	err := errors.New("error")

	hub.RecoverWithContext(ctx, err)

	assertEqual(t, "RecoverWithContext", client.lastCall)
	assertEqual(t, ctx, client.lastCallArgs[0])
	assertEqual(t, err, client.lastCallArgs[1])
	assertEqual(t, scope, client.lastCallArgs[2])
}

func TestFlushShouldPanicTillImplemented(t *testing.T) {
	hub, _, _ := setupHubTest()

	func() {
		defer func() {
			err := recover()
			if err == nil {
				t.Error("flush should panic")
			}
		}()

		hub.Flush(0)
	}()
}

func TestHasHubOnContextReturnsTrueIfHubIsThere(t *testing.T) {
	hub, _, _ := setupHubTest()
	ctx := context.Background()
	ctx = SetHubOnContext(ctx, hub)
	assertEqual(t, true, HasHubOnContext(ctx))
}

func TestHasHubOnContextReturnsFalseIfHubIsNotThere(t *testing.T) {
	ctx := context.Background()
	assertEqual(t, false, HasHubOnContext(ctx))
}

func TestGetHubFromContext(t *testing.T) {
	hub, _, _ := setupHubTest()
	ctx := context.Background()
	ctx = SetHubOnContext(ctx, hub)
	hubFromContext := GetHubFromContext(ctx)
	assertEqual(t, hub, hubFromContext)
}

func TestGetHubFromContextReturnsNilIfHubIsNotThere(t *testing.T) {
	ctx := context.Background()
	hub := GetHubFromContext(ctx)
	if hub != nil {
		t.Error("hub shouldnt be available on empty context")
	}
}

func TestSetHubOnContextReturnsNewContext(t *testing.T) {
	hub, _, _ := setupHubTest()
	ctx := context.Background()
	ctxWithHub := SetHubOnContext(ctx, hub)
	if ctx == ctxWithHub {
		t.Error("contexts should be different")
	}
}
