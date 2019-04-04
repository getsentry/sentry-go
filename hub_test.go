package sentry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewHubPushLayerOnTopOfStack(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}

	hub := NewHub(client, scope)

	assert.Len(*hub.Stack(), 1)
}

func TestNewHubLayerStoresClientAndScope(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}

	hub := NewHub(client, scope)

	assert.Equal(&Layer{client: client, scope: scope}, (*hub.Stack())[0])
}

func TestPushScopeAddsScopeOnTopOfStack(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	hub.PushScope()

	assert.Len(*hub.Stack(), 2)
}

func TestPushScopeInheritsScopeData(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	scope.SetExtra("foo", "bar")
	hub.PushScope()
	scope.SetExtra("baz", "qux")

	assert.False((*hub.Stack())[0].scope == (*hub.Stack())[1].scope, "Scope shouldnt point to the same struct")
	assert.Equal(map[string]interface{}{"foo": "bar", "baz": "qux"}, (*hub.Stack())[0].scope.extra)
	assert.Equal(map[string]interface{}{"foo": "bar"}, (*hub.Stack())[1].scope.extra)
}

func TestPushScopeInheritsClient(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	hub.PushScope()

	assert.True((*hub.Stack())[0].client == (*hub.Stack())[1].client, "Client should be inherited")
}

func TestPopScopeRemovesLayerFromTheStack(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}

	hub := NewHub(client, scope)
	hub.PushScope()
	hub.PushScope()
	hub.PopScope()

	assert.Len(*hub.Stack(), 2)
}

func TestPopScopeCannotRemoveFromEmptyStack(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}

	hub := NewHub(client, scope)

	assert.Len(*hub.Stack(), 1)
	hub.PopScope()
	assert.Len(*hub.Stack(), 0)
	hub.PopScope()
	assert.Len(*hub.Stack(), 0)
}

func TestBindClient(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	hub.PushScope()
	newClient := NewClient()
	hub.BindClient(newClient)

	assert.False(
		(*hub.Stack())[0].client == (*hub.Stack())[1].client,
		"Two stack layers should have different clients bound",
	)
	assert.True((*hub.Stack())[0].client == client, "Stack's parent layer should have old client bound")
	assert.True((*hub.Stack())[1].client == newClient, "Stack's top layer should have new client bound")
}

func TestWithScope(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	hub.WithScope(func() {
		assert.Len(*hub.Stack(), 2)
	})

	assert.Len(*hub.Stack(), 1)
}

func TestWithScopeBindClient(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)

	hub.WithScope(func() {
		newClient := NewClient()
		hub.BindClient(newClient)
		assert.True(hub.StackTop().client == newClient)
	})

	assert.True(hub.StackTop().client == client)
}

func TestWithScopeDirectChanges(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)
	hub.Scope().SetExtra("foo", "bar")

	hub.WithScope(func() {
		hub.Scope().SetExtra("foo", "baz")
		assert.Equal(map[string]interface{}{"foo": "baz"}, hub.StackTop().scope.extra)
	})

	assert.Equal(map[string]interface{}{"foo": "bar"}, hub.StackTop().scope.extra)
}

func TestWithScopeChangesThroughConfigureScope(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)
	hub.Scope().SetExtra("foo", "bar")

	hub.WithScope(func() {
		hub.ConfigureScope(func(scope *Scope) {
			scope.SetExtra("foo", "baz")
		})
		assert.Equal(map[string]interface{}{"foo": "baz"}, hub.StackTop().scope.extra)
	})

	assert.Equal(map[string]interface{}{"foo": "bar"}, hub.StackTop().scope.extra)
}

func TestConfigureScope(t *testing.T) {
	assert := assert.New(t)
	client := NewClient()
	scope := &Scope{}
	hub := NewHub(client, scope)
	hub.Scope().SetExtra("foo", "bar")

	hub.ConfigureScope(func(scope *Scope) {
		scope.SetExtra("foo", "baz")
		assert.Equal(map[string]interface{}{"foo": "baz"}, hub.StackTop().scope.extra)
	})

	assert.Equal(map[string]interface{}{"foo": "baz"}, hub.StackTop().scope.extra)
}
