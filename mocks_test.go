package sentry

import (
	"context"
	"net/http"
)

type ScopeMock struct {
	breadcrumb      *Breadcrumb
	shouldDropEvent bool
}

func (scope *ScopeMock) AddBreadcrumb(breadcrumb *Breadcrumb, limit int) {
	scope.breadcrumb = breadcrumb
}

func (scope *ScopeMock) ApplyToEvent(event *Event, hint *EventHint) *Event {
	if scope.shouldDropEvent {
		return nil
	}
	return event
}

type TransportMock struct {
	lastEvent *Event
}

func (t *TransportMock) Configure(options ClientOptions) {}
func (t *TransportMock) SendEvent(event *Event) (*http.Response, error) {
	t.lastEvent = event
	return nil, nil
}

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
