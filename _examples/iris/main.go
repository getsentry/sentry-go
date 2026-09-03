package main

import (
	"context"
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
		sentry.ScopeFromContext(ctx.Request().Context()).SetTag("someRandomTag", "maybeYouNeedIt")
		ctx.Next()
	})

	app.Get("/", func(ctx iris.Context) {
		scope := sentry.ScopeFromContext(ctx.Request().Context())
		scope.SetTag("unwantedQuery", "someQueryDataMaybe")
		sentry.CaptureMessage(ctx.Request().Context(), "User provided unwanted query string, but we recovered just fine")

		expensiveThing := func(ctx context.Context) {
			span := sentry.StartSpan(ctx, "expensive_thing")
			defer span.Finish()

			// do resource intensive thing
		}

		// Acquire the transaction from the request context. It may be nil if
		// you did not set up the sentryiris middleware.
		spanContext := ctx.Request().Context()
		if sentrySpan := sentry.SpanFromContext(spanContext); sentrySpan != nil {
			spanContext = sentrySpan.Context()
		}
		expensiveThing(spanContext)

		ctx.StatusCode(http.StatusOK)
	})

	app.Get("/foo", func(ctx iris.Context) {
		// sentryiris handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	_ = app.Run(iris.Addr(":3000"))
}
