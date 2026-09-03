package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/getsentry/sentry-go"
	sentryecho "github.com/getsentry/sentry-go/echo"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
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

	app.Use(middleware.RequestLogger())
	app.Use(middleware.Recover())

	app.Use(sentryecho.New(sentryecho.Options{
		Repanic: true,
	}))

	app.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(ctx *echo.Context) error {
			sentry.ScopeFromContext(ctx.Request().Context()).SetTag("someRandomTag", "maybeYouNeedIt")
			return next(ctx)
		}
	})

	app.GET("/", func(ctx *echo.Context) error {
		scope := sentry.ScopeFromContext(ctx.Request().Context())
		scope.SetTag("unwantedQuery", "someQueryDataMaybe")
		sentry.CaptureMessage(ctx.Request().Context(), "User provided unwanted query string, but we recovered just fine")

		expensiveThing := func(ctx context.Context) error {
			span := sentry.StartTransaction(ctx, "expensive_thing")
			defer span.Finish()
			// do resource intensive thing
			return nil
		}

		// Acquire the transaction from the request context. It may be nil if
		// you did not set up the sentryecho middleware.
		spanContext := ctx.Request().Context()
		if sentrySpan := sentry.SpanFromContext(spanContext); sentrySpan != nil {
			spanContext = sentrySpan.Context()
		}
		err := expensiveThing(spanContext)
		if err != nil {
			return err
		}

		return ctx.String(http.StatusOK, "Hello, World!")
	})

	app.GET("/foo", func(ctx *echo.Context) error {
		// sentryecho handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	log.Fatal(app.Start(":3000"))
}
