package sentry

import (
	"context"
)

var _globalHub = NewHub(&Client{}, &Scope{})

type HubError struct {
	Message string
}

func (e HubError) Error() string {
	return "[Sentry] HubError: " + e.Message
}

var getCurrentHub = GetCurrentHub

// TODO: Rewrite this completely
func GetCurrentHub() (*Hub, error) {
	if _globalHub != nil {
		return _globalHub, nil
	}
	client, _ := NewClient(ClientOptions{})
	scope := &Scope{}
	if client == nil {
		return nil, HubError{"No Client available"}
	}
	if scope == nil {
		return nil, HubError{"No Scope available"}
	}
	return NewHub(client, scope), nil
}

func Init(options ClientOptions) error {
	hub, err := getCurrentHub()
	if err != nil {
		return err
	}
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	hub.BindClient(client)
	return nil
}

func AddBreadcrumb(breadcrumb *Breadcrumb) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.AddBreadcrumb(breadcrumb, nil)
}

func CaptureMessage(message string) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.CaptureMessage(message, nil)
}

func CaptureException(exception error) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.CaptureException(exception, &EventHint{OriginalException: exception})
}

func CaptureEvent(event *Event) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.CaptureEvent(event, nil)
}

func Recover() {
	if recoveredErr := recover(); recoveredErr != nil {
		hub, err := getCurrentHub()
		if err != nil {
			return
		}
		hub.Recover(recoveredErr)
	}
}

func RecoverWithContext(ctx context.Context) {
	if recoveredErr := recover(); recoveredErr != nil {
		hub, err := getCurrentHub()
		if err != nil {
			return
		}
		hub.RecoverWithContext(ctx, recoveredErr)
	}
}

// TODO: Or maybe just `Recover(true)`? It may be too generic though
// func RecoverAndPanic() {
// 	if err := recover(); err != nil {
// 		Recover()
// 		panic(err)
// 	}
// }

func WithScope(f func(scope *Scope)) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.WithScope(f)
}

func ConfigureScope(f func(scope *Scope)) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.ConfigureScope(f)
}

func PushScope() {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.PushScope()
}
func PopScope() {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.PopScope()
}

func Flush(timeout int) {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.Flush(timeout)
}

func LastEventID() {
	hub, err := getCurrentHub()
	if err != nil {
		return
	}
	hub.LastEventID()
}
