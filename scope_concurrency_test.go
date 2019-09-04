package sentry_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestConcurrentScopeUsage(t *testing.T) {
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(x int) {
			defer wg.Done()
			sentry.WithScope(func(scope *sentry.Scope) {
				touchScope(scope, x)
			})
		}(i)
		wg.Add(1)
		go func(x int) {
			defer wg.Done()
			sentry.ConfigureScope(func(scope *sentry.Scope) {
				touchScope(scope, x)
			})
		}(i)
	}

	for i := 0; i < 10; i++ {
		func(x int) {
			sentry.WithScope(func(scope *sentry.Scope) {
				touchScope(scope, x)
			})
		}(i)

		func(x int) {
			sentry.ConfigureScope(func(scope *sentry.Scope) {
				touchScope(scope, x)
			})
		}(i)
	}

	// wait for goroutines to finish
	wg.Wait()
}

func touchScope(scope *sentry.Scope, x int) {
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
}
