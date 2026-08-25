package main

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
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

	app := gin.Default()

	app.Use(sentrygin.New(sentrygin.Options{
		Repanic: true,
	}))

	app.Use(func(ctx *gin.Context) {
		sentrygin.GetScopeFromContext(ctx).SetTag("someRandomTag", "maybeYouNeedIt")
		ctx.Next()
	})

	app.GET("/", func(ctx *gin.Context) {
		scope := sentrygin.GetScopeFromContext(ctx)
		scope.SetTag("unwantedQuery", "someQueryDataMaybe")
		sentry.CaptureMessage(ctx.Request.Context(), "User provided unwanted query string, but we recovered just fine")
		ctx.Status(http.StatusOK)
	})

	app.GET("/foo", func(ctx *gin.Context) {
		// sentrygin handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	_ = app.Run(":3000")
}
