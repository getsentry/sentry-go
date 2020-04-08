<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Buffalo Middleware for Sentry-go SDK

**Godoc:** https://godoc.org/github.com/getsentry/sentry-go/buffalo

**Example:** https://github.com/getsentry/sentry-go/tree/master/example/buffalo

## Installation

```sh
go get github.com/getsentry/sentry-go/buffalo
```

```go
package actions

import (
	"github.com/gobuffalo/buffalo"
	"github.com/gobuffalo/envy"
	forcessl "github.com/gobuffalo/mw-forcessl"
	paramlogger "github.com/gobuffalo/mw-paramlogger"
	"github.com/unrolled/secure"
	sentrybuffalo "github.com/getsentry/sentry-go/buffalo"

	"sentrybuffaloexample/models"

	"github.com/gobuffalo/buffalo-pop/pop/popmw"
	contenttype "github.com/gobuffalo/mw-contenttype"
	"github.com/gobuffalo/x/sessions"
	"github.com/rs/cors"
)

var ENV = envy.Get("GO_ENV", "development")
var app *buffalo.App

func App() *buffalo.App {
	if app == nil {
		app = buffalo.New(buffalo.Options{
			Env:          ENV,
			SessionStore: sessions.Null{},
			PreWares: []buffalo.PreWare{
				cors.Default().Handler,
			},
			SessionName: "_sentrybuffaloexample_session",
		})

        // To initialize Sentry's handler, you need to initialize Sentry itself beforehand
		buildInfo := runtime.Build()
		if err := sentry.Init(sentry.ClientOptions{
			Release:          buildInfo.Version,
			Dist:             buildInfo.Time.String(),
		}); err != nil {
			app.Logger().Errorf("Sentry initialization failed: %v\n", err)
		}

        // Attach the integration as one of your middleware
		app.Use(sentrybuffalo.New(sentrybuffalo.Options{
			Repanic: true,
		}))

		// Automatically redirect to SSL
		app.Use(forceSSL())

		// Log request parameters (filters apply).
		app.Use(paramlogger.ParameterLogger)

		// Set the request content type to JSON
		app.Use(contenttype.Set("application/json"))

		// Wraps each request in a transaction.
		//  c.Value("tx").(*pop.Connection)
		// Remove to disable this.
		app.Use(popmw.Transaction(models.DB))

		app.GET("/", HomeHandler)
	}

	return app
}
```

## Configuration

`sentrybuffalo` accepts a struct of `Options` that allows you to configure how the handler will behave.

Currently it respects 3 options:

```go
// Repanic configures whether Sentry should repanic after recovery
Repanic bool
// WaitForDelivery indicates whether to wait until panic details have been
// sent to Sentry before panicking or proceeding with a request.
WaitForDelivery bool
// Timeout for the event delivery requests.
Timeout time.Duration
// CaptureError will capture the error if one was returned.
CaptureError bool
```

## Usage

`sentrybuffalo` attaches an instance of `*sentry.Hub` (https://godoc.org/github.com/getsentry/sentry-go#Hub) to the `buffalo.Context`, which makes it available throughout the rest of the request's lifetime.
You can access it by using the `sentrybuffalo.GetHubFromContext()` method on the context itself in any of your proceeding middleware and routes.
And it should be used instead of the global `sentry.CaptureMessage`, `sentry.CaptureException`, or any other calls, as it keeps the separation of data between the requests.

**Keep in mind that `*sentry.Hub` won't be available in middleware attached before to `sentrybuffalo`!**

```go
app = buffalo.New(buffalo.Options{
    Env:          ENV,
    SessionStore: sessions.Null{},
    PreWares: []buffalo.PreWare{
        cors.Default().Handler,
    },
    SessionName: "_sentrybuffaloexample_session",
})

// To initialize Sentry's handler, you need to initialize Sentry itself beforehand
buildInfo := runtime.Build()
if err := sentry.Init(sentry.ClientOptions{
    Release:          buildInfo.Version,
    Dist:             buildInfo.Time.String(),
}); err != nil {
    app.Logger().Errorf("Sentry initialization failed: %v\n", err)
}
app.Use(sentrybuffalo.New(sentrybuffalo.Options{
	Repanic: true,
}))

app.Use(func(next buffalo.Handler) buffalo.Handler {
	return func(c buffalo.Context) error {

		if hub := sentrybuffalo.GetHubFromContext(c); hub != nil {

			if requestIDValue := c.Value("request_id"); requestIDValue != nil {
				requestID := requestIDValue.(string)
				hub.ConfigureScope(func(scope *sentry.Scope) {
					scope.SetExtra("request_id", requestID)
				})
			}
		}

		return next(c)
	}
}

app.GET("/", func(c buffalo.Context) error {
	if hub := sentrybuffalo.GetHubFromContext(c); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
			hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
		})
	}
	return c.Render(http.StatusOK, r.JSON(map[string]string{"message": "Welcome to Buffalo!"}))
})

app.GET("/foo", func(c buffalo.Context) error {
	// sentrybuffalo handler will catch it just fine. Also, because we attached "request_id"
	// in the middleware before, it will be sent through as well
	panic("y tho")
})

```

### Accessing Request in `BeforeSend` callback

```go
sentry.Init(sentry.ClientOptions{
    Dsn: "your-public-dsn",
    BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
        if hint.Context != nil {
            if req, ok := hint.Context.Value(sentry.RequestContextKey).(*http.Request); ok {
                // You have access to the original Request here
            }
        }

        return event
    },
})
```
