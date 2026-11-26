package main

import (
	"fmt"

	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/getsentry/sentry-go/fiber"
	"github.com/gofiber/fiber/v2"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if hint.Request != nil {
				// You have access to the request
				fmt.Println(hint.Request.Host)
			}
			fmt.Println(event)
			return event
		},
		Debug:            true,
		AttachStacktrace: true,
	})

	// Later in the code
	sentryHandler := sentryfiber.New(sentryfiber.Options{
		Repanic:         true,
		WaitForDelivery: true,
	})

	enhanceSentryEvent := func(ctx *fiber.Ctx) error {
		sentry.ScopeFromContext(ctx.UserContext()).SetTag("someRandomTag", "maybeYouNeedIt")
		return ctx.Next()
	}

	app := fiber.New()

	app.Use(sentryHandler)

	app.All("/foo", enhanceSentryEvent, func(c *fiber.Ctx) error {
		panic("y tho")
	})

	app.All("/", func(ctx *fiber.Ctx) error {
		captureCtx, captureScope := sentry.WithScope(ctx.UserContext())
		captureScope.SetTag("unwantedQuery", "someQueryDataMaybe")
		sentry.CaptureMessage(captureCtx, "User provided unwanted query string, but we recovered just fine")
		return ctx.SendStatus(fiber.StatusOK)
	})

	if err := app.Listen(":3000"); err != nil {
		panic(err)
	}
}
