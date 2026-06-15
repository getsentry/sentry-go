<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Fiber v3 Handler for Sentry-go SDK

**Godoc:** https://godoc.org/github.com/getsentry/sentry-go/fiberv3

## Installation

```sh
go get github.com/getsentry/sentry-go/fiberv3
```

```go
import (
	"fmt"
	fiber "github.com/gofiber/fiber/v3"
	"github.com/getsentry/sentry-go"
	sentryfiber "github.com/getsentry/sentry-go/fiberv3"
)
```

To initialize Sentry's handler, you need to initialize Sentry itself beforehand.

```go
if err := sentry.Init(sentry.ClientOptions{
	Dsn: "your-public-dsn",
}); err != nil {
	fmt.Printf("Sentry initialization failed: %v\n", err)
}

sentryHandler := sentryfiber.New(sentryfiber.Options{})

app := fiber.New()
app.Use(sentryHandler)
app.Listen(":3000")
```

## Configuration

`sentryfiber` accepts a struct of `Options` that allows you to configure how the handler will behave.

```go
// Repanic configures whether Sentry should repanic after recovery, in most cases it should be set to false,
// as fasthttp doesn't include its own Recovery handler.
Repanic bool
// WaitForDelivery configures whether you want to block the request before moving forward with the response.
// Because fasthttp doesn't include its own `Recovery` handler, it will restart the application,
// and event won't be delivered otherwise.
WaitForDelivery bool
// Timeout for the event delivery requests.
Timeout time.Duration
```

## Usage

`sentryfiber` attaches an instance of `*sentry.Hub` to the request context, which makes it available throughout the rest of the request's lifetime.
You can access it by using `sentryfiber.GetHubFromContext()` in any subsequent middleware and routes.

**Keep in mind that `*sentry.Hub` won't be available in middleware attached before `sentryfiber`.**

```go
sentryHandler := sentryfiber.New(sentryfiber.Options{
	Repanic:         true,
	WaitForDelivery: true,
})

enhanceSentryEvent := func(ctx fiber.Ctx) error {
	if hub := sentryfiber.GetHubFromContext(ctx); hub != nil {
		hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
	}
	return ctx.Next()
}

app := fiber.New()
app.Use(sentryHandler)

app.All("/foo", enhanceSentryEvent, func(ctx fiber.Ctx) error {
	panic("y tho")
})

app.All("/", func(ctx fiber.Ctx) error {
	if hub := sentryfiber.GetHubFromContext(ctx); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("unwantedQuery", "someQueryDataMaybe")
			hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
		})
	}
	return ctx.SendStatus(fiber.StatusOK)
})

app.Listen(":3000")
```

### Accessing Context in `BeforeSend` callback

```go
sentry.Init(sentry.ClientOptions{
	Dsn: "your-public-dsn",
	BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
		if hint.Context != nil {
			if ctx, ok := hint.Context.Value(sentry.RequestContextKey).(fiber.Ctx); ok {
				fmt.Println(ctx.Hostname())
			}
		}
		return event
	},
})
```
