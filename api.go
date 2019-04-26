package sentry

import (
	"context"
)

func Init(options ClientOptions) error {
	hub := GlobalHub()
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	hub.BindClient(client)
	return nil
}

func AddBreadcrumb(breadcrumb *Breadcrumb) {
	hub := GlobalHub()
	hub.AddBreadcrumb(breadcrumb, nil)
}

func CaptureMessage(message string) {
	hub := GlobalHub()
	hub.CaptureMessage(message, nil)
}

func CaptureException(exception error) {
	hub := GlobalHub()
	hub.CaptureException(exception, &EventHint{OriginalException: exception})
}

func CaptureEvent(event *Event) {
	hub := GlobalHub()
	hub.CaptureEvent(event, nil)
}

func Recover() {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := GlobalHub()
		hub.Recover(recoveredErr)
	}
}

func RecoverWithContext(ctx context.Context) {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := GlobalHub()
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
	hub := GlobalHub()
	hub.WithScope(f)
}

func ConfigureScope(f func(scope *Scope)) {
	hub := GlobalHub()
	hub.ConfigureScope(f)
}

func PushScope() {
	hub := GlobalHub()
	hub.PushScope()
}
func PopScope() {
	hub := GlobalHub()
	hub.PopScope()
}

func Flush(timeout int) {
	hub := GlobalHub()
	hub.Flush(timeout)
}

func LastEventID() {
	hub := GlobalHub()
	hub.LastEventID()
}
