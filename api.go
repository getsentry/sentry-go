package sentry

import (
	"context"
)

var _globalHub = NewHub(nil, &Scope{})

func GetGlobalHub() *Hub {
	return _globalHub
}

func Init(options ClientOptions) error {
	hub := GetGlobalHub()
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	hub.BindClient(client)
	return nil
}

func AddBreadcrumb(breadcrumb *Breadcrumb) {
	hub := GetGlobalHub()
	hub.AddBreadcrumb(breadcrumb, nil)
}

func CaptureMessage(message string) {
	hub := GetGlobalHub()
	hub.CaptureMessage(message, nil)
}

func CaptureException(exception error) {
	hub := GetGlobalHub()
	hub.CaptureException(exception, &EventHint{OriginalException: exception})
}

func CaptureEvent(event *Event) {
	hub := GetGlobalHub()
	hub.CaptureEvent(event, nil)
}

func Recover() {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := GetGlobalHub()
		hub.Recover(recoveredErr)
	}
}

func RecoverWithContext(ctx context.Context) {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := GetGlobalHub()
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
	hub := GetGlobalHub()
	hub.WithScope(f)
}

func ConfigureScope(f func(scope *Scope)) {
	hub := GetGlobalHub()
	hub.ConfigureScope(f)
}

func PushScope() {
	hub := GetGlobalHub()
	hub.PushScope()
}
func PopScope() {
	hub := GetGlobalHub()
	hub.PopScope()
}

func Flush(timeout int) {
	hub := GetGlobalHub()
	hub.Flush(timeout)
}

func LastEventID() {
	hub := GetGlobalHub()
	hub.LastEventID()
}
