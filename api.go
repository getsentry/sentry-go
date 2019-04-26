package sentry

import (
	"context"
)

func Init(options ClientOptions) error {
	hub := CurrentHub()
	client, err := NewClient(options)
	if err != nil {
		return err
	}
	hub.BindClient(client)
	return nil
}

func AddBreadcrumb(breadcrumb *Breadcrumb) {
	hub := CurrentHub()
	hub.AddBreadcrumb(breadcrumb, nil)
}

func CaptureMessage(message string) {
	hub := CurrentHub()
	hub.CaptureMessage(message, nil)
}

func CaptureException(exception error) {
	hub := CurrentHub()
	hub.CaptureException(exception, &EventHint{OriginalException: exception})
}

func CaptureEvent(event *Event) {
	hub := CurrentHub()
	hub.CaptureEvent(event, nil)
}

func Recover() {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := CurrentHub()
		hub.Recover(recoveredErr)
	}
}

func RecoverWithContext(ctx context.Context) {
	if recoveredErr := recover(); recoveredErr != nil {
		hub := CurrentHub()
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
	hub := CurrentHub()
	hub.WithScope(f)
}

func ConfigureScope(f func(scope *Scope)) {
	hub := CurrentHub()
	hub.ConfigureScope(f)
}

func PushScope() {
	hub := CurrentHub()
	hub.PushScope()
}
func PopScope() {
	hub := CurrentHub()
	hub.PopScope()
}

func Flush(timeout int) {
	hub := CurrentHub()
	hub.Flush(timeout)
}

func LastEventID() {
	hub := CurrentHub()
	hub.LastEventID()
}
