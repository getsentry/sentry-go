package sentry

import (
	"context"
)

// Default maximum number of breadcrumbs added to an event. Can be overwritten `maxBreadcrumbs` option.
const DefaultMaxBreadcrumbs = 30

// Absolute maximum number of breadcrumbs added to an event.
// The `maxBreadcrumbs` option cannot be higher than this value.
const MaxBreadcrumbs = 100

type ctxKey int

const HubCtxKey = ctxKey(42)

type Hub struct {
	stack       *Stack
	lastEventID string
}

type Layer struct {
	client *Client
	scope  *Scope
}

type Stack []*Layer

func NewHub(client *Client, scope *Scope) *Hub {
	return &Hub{
		stack: &Stack{{
			client: client,
			scope:  scope,
		}},
	}
}

func (hub *Hub) LastEventID() string {
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

func (hub *Hub) Client() *Client {
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

func (hub *Hub) BindClient(client *Client) {
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

func (hub *Hub) invokeClient(callback func(client *Client, scope *Scope)) {
	client, scope := hub.Client(), hub.Scope()
	if client == nil || scope == nil {
		return
	}
	callback(client, scope)
}

func (hub *Hub) CaptureEvent(event *Event, hint *EventHint) {
	hub.invokeClient(func(client *Client, scope *Scope) {
		client.CaptureEvent(event, hint, scope)
	})
}

func (hub *Hub) CaptureMessage(message string, hint *EventHint) {
	hub.invokeClient(func(client *Client, scope *Scope) {
		client.CaptureMessage(message, hint, scope)
	})
}

func (hub *Hub) CaptureException(exception error, hint *EventHint) {
	hub.invokeClient(func(client *Client, scope *Scope) {
		client.CaptureException(exception, hint, scope)
	})
}

func (hub *Hub) AddBreadcrumb(breadcrumb *Breadcrumb, hint *BreadcrumbHint) {
	options := hub.Client().Options()
	maxBreadcrumbs := DefaultMaxBreadcrumbs

	if options.MaxBreadcrumbs != 0 {
		maxBreadcrumbs = options.MaxBreadcrumbs
	}

	if maxBreadcrumbs < 0 {
		return
	}

	if options.BeforeBreadcrumb != nil {
		h := &BreadcrumbHint{}
		if hint != nil {
			h = hint
		}
		if breadcrumb = options.BeforeBreadcrumb(breadcrumb, h); breadcrumb == nil {
			debugger.Println("breadcrumb dropped due to BeforeBreadcrumb callback")
			return
		}
	}

	max := maxBreadcrumbs
	if max > MaxBreadcrumbs {
		max = MaxBreadcrumbs
	}
	hub.Scope().AddBreadcrumb(breadcrumb, max)
}

func (hub *Hub) Recover(recoveredErr interface{}) {
	hub.invokeClient(func(client *Client, scope *Scope) {
		client.Recover(recoveredErr, scope)
	})
}

func (hub *Hub) RecoverWithContext(ctx context.Context, recoveredErr interface{}) {
	hub.invokeClient(func(client *Client, scope *Scope) {
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
