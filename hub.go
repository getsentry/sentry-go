package sentry

import (
	"github.com/google/uuid"
)

// TODO: Expose only necessary methods

type Layer struct {
	client *Client
	scope  *Scope
}

type Stack []*Layer

type Hub struct {
	stack       *Stack
	lastEventID uuid.UUID
}

func NewHub(client *Client, scope *Scope) *Hub {
	return &Hub{
		stack: &Stack{{
			client: client,
			scope:  scope,
		}},
	}
}

func (hub Hub) LastEventID() uuid.UUID {
	return hub.lastEventID
}

func (hub Hub) Stack() *Stack {
	return hub.stack
}

func (hub Hub) StackTop() *Layer {
	return (*hub.stack)[len(*hub.stack)-1]
}

func (hub Hub) Scope() *Scope {
	return hub.StackTop().scope
}

func (hub Hub) Client() *Client {
	return hub.StackTop().client
}

func (hub *Hub) PushScope() *Scope {
	scope := hub.Scope().Clone()

	*hub.stack = append(*hub.Stack(), &Layer{
		client: hub.Client(),
		scope:  scope,
	})

	return scope
}

func (hub *Hub) PopScope() {
	stack := *hub.Stack()
	if len(stack) == 0 {
		return
	}
	*hub.stack = stack[0 : len(stack)-1]
}

func (hub *Hub) BindClient(client *Client) {
	hub.StackTop().client = client
}

func (hub *Hub) WithScope(f func()) {
	hub.PushScope()
	defer hub.PopScope()
	f()
}

func (hub *Hub) ConfigureScope(f func(scope *Scope)) {
	f(hub.Scope())
}
