package main

import (
	"fmt"
	"github.com/getsentry/sentry-go"
	sentryFiber "github.com/getsentry/sentry-go/fiber"
	"github.com/gofiber/fiber/v2"
	"log"
)

func enhanceSentryEvent(handler fiber.Handler) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		if hub := sentryFiber.GetHubFromContext(ctx); hub != nil {
			hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
		}
		if err := handler(ctx); err != nil {
			return err
		}
		return nil
	}
}

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn: "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			if hint.Context != nil {
				if ctx, ok := hint.Context.Value(sentry.RequestContextKey).(*fiber.Ctx); ok {
					fmt.Println(string(ctx.Request().Host()))
				}
			}
			fmt.Println(event)
			return event
		},
		Debug:            true,
		AttachStacktrace: true,
	})
	sentryHandler := sentryFiber.New(sentryFiber.Options{})
	defaultHandler := func(ctx *fiber.Ctx) {
		if hub := sentryFiber.GetHubFromContext(ctx); hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
				hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
			})
		}
		ctx.Status(fiber.StatusOK)
	}
	fooHandler := enhanceSentryEvent(func(ctx *fiber.Ctx) error {
		panic("test panic")
	})

	fiberHandler := func(ctx *fiber.Ctx) error {
		switch ctx.Path() {
		case "/foo":
			if err := fooHandler(ctx); err != nil {
				return err
			}
		default:
			defaultHandler(ctx)
		}
		return nil
	}

	fmt.Println("Listening and serving HTTP on :3000")
	handler := sentryHandler.Handle(fiberHandler)
	app := fiber.New()
	app.Use(handler)
	if err := app.Listen(":3000"); err != nil {
		log.Fatalln(err)
	}
}
