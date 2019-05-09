package sentry

import (
	"context"
	"testing"
)

func setupHubTest() (*Hub, *Client, *Scope) {
	client := &Client{}
	scope := &Scope{}
	hub := NewHub(client, scope)
	return hub, client, scope
}

func TestNewHubPushesLayerOnTopOfStack(t *testing.T) {
	hub, _, _ := setupHubTest()
	assertEqual(t, len(*hub.stack), 1)
}

func TestNewHubLayerStoresClientAndScope(t *testing.T) {
	hub, client, scope := setupHubTest()
	assertEqual(t, &Layer{client: client, scope: scope}, (*hub.stack)[0])
}

func TestPushScopeAddsScopeOnTopOfStack(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.PushScope()
	assertEqual(t, len(*hub.stack), 2)
}

func TestPushScopeInheritsScopeData(t *testing.T) {
	hub, _, scope := setupHubTest()
	scope.SetExtra("foo", "bar")
	hub.PushScope()
	scope.SetExtra("baz", "qux")

	if (*hub.stack)[0].scope == (*hub.stack)[1].scope {
		t.Error("Scope shouldnt point to the same struct")
	}
	assertEqual(t, map[string]interface{}{"foo": "bar", "baz": "qux"}, (*hub.stack)[0].scope.extra)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, (*hub.stack)[1].scope.extra)
}

func TestPushScopeInheritsClient(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.PushScope()

	if (*hub.stack)[0].client != (*hub.stack)[1].client {
		t.Error("Client should be inherited")
	}
}

func TestPopScopeRemovesLayerFromTheStack(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.PushScope()
	hub.PushScope()
	hub.PopScope()

	assertEqual(t, len(*hub.stack), 2)
}

func TestPopScopeCannotRemoveFromEmptyStack(t *testing.T) {
	hub, _, _ := setupHubTest()
	assertEqual(t, len(*hub.stack), 1)
	hub.PopScope()
	assertEqual(t, len(*hub.stack), 0)
	hub.PopScope()
	assertEqual(t, len(*hub.stack), 0)
}

func TestBindClient(t *testing.T) {
	hub, client, _ := setupHubTest()
	hub.PushScope()
	newClient := &Client{}
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

func TestWithScopeCreatesIsolatedScope(t *testing.T) {
	hub, _, _ := setupHubTest()

	hub.WithScope(func(scope *Scope) {
		assertEqual(t, len(*hub.stack), 2)
	})

	assertEqual(t, len(*hub.stack), 1)
}

func TestWithScopeBindClient(t *testing.T) {
	hub, client, _ := setupHubTest()

	hub.WithScope(func(scope *Scope) {
		newClient := &Client{}
		hub.BindClient(newClient)
		if hub.stackTop().client != newClient {
			t.Error("should use newly bound client")
		}
	})

	if hub.stackTop().client != client {
		t.Error("should use old client")
	}
}

func TestWithScopeDirectChanges(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.Scope().SetExtra("foo", "bar")

	hub.WithScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
	})

	assertEqual(t, map[string]interface{}{"foo": "bar"}, hub.stackTop().scope.extra)
}

func TestWithScopeChangesThroughConfigureScope(t *testing.T) {
	hub, _, _ := setupHubTest()
	hub.Scope().SetExtra("foo", "bar")

	hub.WithScope(func(scope *Scope) {
		hub.ConfigureScope(func(scope *Scope) {
			scope.SetExtra("foo", "baz")
		})
		assertEqual(t, map[string]interface{}{"foo": "baz"}, hub.stackTop().scope.extra)
	})

	assertEqual(t, map[string]interface{}{"foo": "bar"}, hub.stackTop().scope.extra)
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

func TestLayerAccessingEmptyStack(t *testing.T) {
	hub := &Hub{}
	if hub.stackTop() != nil {
		t.Error("expected nil to be returned")
	}
}

func TestLayerAccessingScopeReturnsNilIfStackIsEmpty(t *testing.T) {
	hub := &Hub{}
	if hub.Scope() != nil {
		t.Error("expected nil to be returned")
	}
}

func TestLayerAccessingClientReturnsNilIfStackIsEmpty(t *testing.T) {
	hub := &Hub{}
	if hub.Client() != nil {
		t.Error("expected nil to be returned")
	}
}

func TestAddBreadcrumbRespectMaxBreadcrumbsOption(t *testing.T) {
	hub, client, scope := setupHubTest()
	client.options.MaxBreadcrumbs = 2

	breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

	hub.AddBreadcrumb(breadcrumb, nil)
	hub.AddBreadcrumb(breadcrumb, nil)
	hub.AddBreadcrumb(breadcrumb, nil)

	assertEqual(t, len(scope.breadcrumbs), 2)
}

func TestAddBreadcrumbSkipAllBreadcrumbsIfMaxBreadcrumbsIsLessThanZero(t *testing.T) {
	hub, client, scope := setupHubTest()
	client.options.MaxBreadcrumbs = -1

	breadcrumb := &Breadcrumb{Message: "Breadcrumb"}

	hub.AddBreadcrumb(breadcrumb, nil)
	hub.AddBreadcrumb(breadcrumb, nil)
	hub.AddBreadcrumb(breadcrumb, nil)

	assertEqual(t, len(scope.breadcrumbs), 0)
}

func TestAddBreadcrumbCallsBeforeBreadcrumbCallback(t *testing.T) {
	hub, client, scope := setupHubTest()
	client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
		breadcrumb.Message += "_wat"
		return breadcrumb
	}

	hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

	assertEqual(t, len(scope.breadcrumbs), 1)
	assertEqual(t, "Breadcrumb_wat", scope.breadcrumbs[0].Message)
}

func TestBeforeBreadcrumbCallbackCanDropABreadcrumb(t *testing.T) {
	hub, client, scope := setupHubTest()
	client.options.BeforeBreadcrumb = func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb {
		return nil
	}

	hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)
	hub.AddBreadcrumb(&Breadcrumb{Message: "Breadcrumb"}, nil)

	assertEqual(t, len(scope.breadcrumbs), 0)
}

func TestBeforeBreadcrumbGetAccessToEventHint(t *testing.T) {
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
}

func TestInvokeClientExecutesCallbackWithClientAndScopePassed(t *testing.T) {
	hub, _, _ := setupHubTest()
	callback := func(client *Client, scope *Scope) {
		assertEqual(t, client, client)
		assertEqual(t, scope, scope)
	}
	hub.invokeClient(callback)
}

func TestInvokeClientFailsSilentlyWHenNoClientOrScopeAvailable(t *testing.T) {
	hub := &Hub{}
	callback := func(_ *Client, _ *Scope) {
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
