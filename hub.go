package sentry

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey int

const HubCtxKey = ctxKey(42)

type Layer struct {
	client Clienter
	scope  *Scope
}

type Stack []*Layer

type Hub struct {
	stack       *Stack
	lastEventID uuid.UUID
}

func NewHub(client Clienter, scope *Scope) *Hub {
	return &Hub{
		stack: &Stack{{
			client: client,
			scope:  scope,
		}},
	}
}

func (hub *Hub) LastEventID() uuid.UUID {
	return hub.lastEventID
}

func (hub *Hub) stackTop() *Layer {
	stack := hub.stack
	if stack == nil || len(*stack) == 0 {
		return nil
	}
	return (*stack)[len(*stack)-1]
}

func (hub *Hub) Scope() *Scope {
	top := hub.stackTop()
	if top == nil {
		return nil
	}
	return top.scope
}

func (hub *Hub) Client() Clienter {
	top := hub.stackTop()
	if top == nil {
		return nil
	}
	return top.client
}

func (hub *Hub) PushScope() *Scope {
	scope := hub.Scope().Clone()

	*hub.stack = append(*hub.stack, &Layer{
		client: hub.Client(),
		scope:  scope,
	})

	return scope
}

func (hub *Hub) PopScope() {
	stack := *hub.stack
	if len(stack) == 0 {
		return
	}
	*hub.stack = stack[0 : len(stack)-1]
}

func (hub *Hub) BindClient(client Clienter) {
	hub.stackTop().client = client
}

func (hub *Hub) WithScope(f func(scope *Scope)) {
	scope := hub.PushScope()
	defer hub.PopScope()
	f(scope)
}

func (hub *Hub) ConfigureScope(f func(scope *Scope)) {
	f(hub.Scope())
}

func (hub *Hub) invokeClient(callback func(client Clienter, scope *Scope)) {
	client, scope := hub.Client(), hub.Scope()
	if client == nil || scope == nil {
		return
	}
	callback(client, scope)
}

func (hub *Hub) CaptureEvent(event *Event) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.CaptureEvent(event, scope)
	})
}

func (hub *Hub) CaptureMessage(message string) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.CaptureMessage(message, scope)
	})
}

func (hub *Hub) CaptureException(exception error) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.CaptureException(exception, scope)
	})
}

func (hub *Hub) AddBreadcrumb(breadcrumb *Breadcrumb) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.AddBreadcrumb(breadcrumb, scope)
	})
}

func (hub *Hub) Recover(recoveredErr interface{}) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.Recover(recoveredErr, scope)
	})
}

func (hub *Hub) RecoverWithContext(ctx context.Context, recoveredErr interface{}) {
	hub.invokeClient(func(client Clienter, scope *Scope) {
		client.RecoverWithContext(ctx, recoveredErr, scope)
	})
}

func (hub *Hub) Flush(timeout int) {
	panic("Implement Flush redirect to the Client")
}

func HasHubOnContext(ctx context.Context) bool {
	_, ok := ctx.Value(HubCtxKey).(*Hub)
	return ok
}

func GetHubFromContext(ctx context.Context) *Hub {
	if hub, ok := ctx.Value(HubCtxKey).(*Hub); ok {
		return hub
	}
	return nil
}

func SetHubOnContext(ctx context.Context, hub *Hub) context.Context {
	return context.WithValue(ctx, HubCtxKey, hub)
}
