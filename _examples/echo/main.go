package main

import (
	"fmt"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
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

	app := echo.New()

	app.Use(middleware.Logger())
	app.Use(middleware.Recover())

	app.Use(sentryecho.New(sentryecho.Options{
		Repanic: true,
	}))

	app.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx echo.Context) error {
			if hub := sentryecho.GetHubFromContext(ctx); hub != nil {
				hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
			}
			return next(ctx)
		}
	})

	app.GET("/", func(ctx echo.Context) error {
		if hub := sentryecho.GetHubFromContext(ctx); hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
				hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
			})
		}
		return ctx.String(http.StatusOK, "Hello, World!")
	})

	app.GET("/foo", func(ctx echo.Context) error {
		// sentryecho handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	app.Logger.Fatal(app.Start(":3000"))
}
