package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	i := 0

	if err := sentry.Init(sentry.ClientOptions{
		Dsn: "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			i++
			if i%1000 == 0 {
				fmt.Println(i)
			}
			return nil
		},
	}); err != nil {
		fmt.Println(err)
	}

	for i := 0; i < 10000; i++ {
		go func(x int) {
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("foo", "bar")
				scope.SetContext("foo", "bar")
				scope.SetExtra("foo", "bar")
				scope.SetLevel(sentry.LevelDebug)
				scope.SetTransaction("foo")
				scope.SetFingerprint([]string{"foo"})
				scope.AddBreadcrumb(&sentry.Breadcrumb{Timestamp: 1337, Message: "foo"}, 100)
				scope.SetUser(sentry.User{ID: "foo"})
				scope.SetRequest(sentry.Request{URL: "foo"})

				sentry.CaptureException(errors.New(string(x)))
			})
		}(i)

		go func(x int) {
			sentry.ConfigureScope(func(scope *sentry.Scope) {
				scope.SetTag("foo", "bar")
				scope.SetContext("foo", "bar")
				scope.SetExtra("foo", "bar")
				scope.SetLevel(sentry.LevelDebug)
				scope.SetTransaction("foo")
				scope.SetFingerprint([]string{"foo"})
				scope.AddBreadcrumb(&sentry.Breadcrumb{Timestamp: 1337, Message: "foo"}, 100)
				scope.SetUser(sentry.User{ID: "foo"})
				scope.SetRequest(sentry.Request{URL: "foo"})

				sentry.CaptureException(errors.New(string(x)))
			})
		}(i)
	}

	for i := 0; i < 10000; i++ {
		func(x int) {
			sentry.WithScope(func(scope *sentry.Scope) {
				scope.SetTag("foo", "bar")
				scope.SetContext("foo", "bar")
				scope.SetExtra("foo", "bar")
				scope.SetLevel(sentry.LevelDebug)
				scope.SetTransaction("foo")
				scope.SetFingerprint([]string{"foo"})
				scope.AddBreadcrumb(&sentry.Breadcrumb{Timestamp: 1337, Message: "foo"}, 100)
				scope.SetUser(sentry.User{ID: "foo"})
				scope.SetRequest(sentry.Request{URL: "foo"})

				sentry.CaptureException(errors.New(string(x)))
			})
		}(i)

		func(x int) {
			sentry.ConfigureScope(func(scope *sentry.Scope) {
				scope.SetTag("foo", "bar")
				scope.SetContext("foo", "bar")
				scope.SetExtra("foo", "bar")
				scope.SetLevel(sentry.LevelDebug)
				scope.SetTransaction("foo")
				scope.SetFingerprint([]string{"foo"})
				scope.AddBreadcrumb(&sentry.Breadcrumb{Timestamp: 1337, Message: "foo"}, 100)
				scope.SetUser(sentry.User{ID: "foo"})
				scope.SetRequest(sentry.Request{URL: "foo"})

				sentry.CaptureException(errors.New(string(x)))
			})
		}(i)
	}

	// wait for goroutines to finish
	time.Sleep(time.Second)
}
