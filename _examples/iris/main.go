//go:build go1.13
// +build go1.13

package main

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/kataras/iris/v12"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if hint.Context != nil {
				if req, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
					// You have access to the original Request
					fmt.Println(req)
				}
			}
			fmt.Println(event)
			return event
		},
		Debug:            true,
		AttachStacktrace: true,
	})

	app := iris.Default()

	app.Use(sentryiris.New(sentryiris.Options{
		Repanic: true,
	}))

	app.Use(func(ctx iris.Context) {
		if hub := sentryiris.GetHubFromContext(ctx); hub != nil {
			hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
		}
		ctx.Next()
	})

	app.Get("/", func(ctx iris.Context) {
		if hub := sentryiris.GetHubFromContext(ctx); hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
				hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
			})
		}
	})

	app.Get("/foo", func(ctx iris.Context) {
		// sentryiris handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	_ = app.Run(iris.Addr(":3000"))
}
