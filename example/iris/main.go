package main

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryiris "github.com/getsentry/sentry-go/iris"
	"github.com/kataras/iris"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
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
		ctx.StatusCode(200)
	})

	app.Get("/foo", func(ctx iris.Context) {
		// sentryiris handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	app.Run(iris.Addr(":3000"))
}
