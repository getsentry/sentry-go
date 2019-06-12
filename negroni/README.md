<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry Negroni Handler for Sentry-go SDK

Godoc: https://godoc.org/github.com/getsentry/sentry-go/negroni
Example: https://github.com/getsentry/sentry-go/tree/master/example/negroni

## Installation

```sh
go get github.com/getsentry/sentry-go/negroni
```

```go
import (
    "fmt"
    "net/http"
    sentrynegroni "github.com/getsentry/sentry-go/negroni"
    "github.com/getsentry/sentry-go"
    "github.com/urfave/negroni"
)

// In order to initialize Sentry's handler, you need to initialize Sentry itself beforehand
if err := sentry.Init(sentry.ClientOptions{
    Dsn: "your-public-dsn",
}); err != nil {
    fmt.Printf("Sentry initialization failed: %v\n", err)
}

// Then create your app
app := negroni.Classic()

// Once it's done, you can attach the handler as one of your middlewares
app.Use(sentrynegroni.New(sentrynegroni.Options{}))

// Setup routes
mux := http.NewServeMux()

mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
    fmt.Fprintf(w, "Hello world!")
})

app.UseHandler(mux)

// And run it
http.ListenAndServe(":3000", app)
```

## Configuration

`sentrynegroni` accepts a struct of `Options` that allows you to configure how the handler will behave.

Currently it respects 3 options:

```go
// Whether Sentry should repanic after recovery, in most cases it should be set to true,
// as negroni.Classic includes it's own Recovery middleware what handles http responses.
Repanic         bool
// Whether you want to block the request before moving forward with the response.
// Because Negroni's default `Recovery` handler doesn't restart the application,
// it's safe to either skip this option or set it to `false`.
WaitForDelivery bool
// Timeout for the event delivery requests.
Timeout         time.Duration
```

## Usage

`sentrynegroni` attaches an instance of `*sentry.Hub` (https://godoc.org/github.com/getsentry/sentry-go#Hub) to the request's context, which makes it available throughout the rest of request's lifetime.
You can access it by using `sentry.GetHubFromContext()` method on the request itself in any of your proceeding middlewares and routes.
And it should be used instead of global `sentry.CaptureMessage`, `sentry.CaptureException` or any other calls, as it keeps the separation of data between the requests.

**Keep in mind that `*sentry.Hub` won't be available in middlewares attached prior to `sentrynegroni`!**

```go
app := negroni.Classic()

app.Use(sentrynegroni.New(sentrynegroni.Options{
    Repanic: true,
}))

app.Use(negroni.HandlerFunc(func(rw http.ResponseWriter, r *http.Request, next http.HandlerFunc) {
    hub := sentry.GetHubFromContext(r.Context())
    hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
    next(rw, r)
}))

mux := http.NewServeMux()

mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
    hub := sentry.GetHubFromContext(r.Context())
    hub.WithScope(func(scope *sentry.Scope) {
        scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
        hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
    })
    rw.WriteHeader(200)
})

mux.HandleFunc("/foo", func(rw http.ResponseWriter, r *http.Request) {
    // sentrynagroni handler will catch it just fine, and because we attached "someRandomTag"
    // in the middleware before, it will be sent through as well
    panic("y tho")
})

app.UseHandler(mux)

http.ListenAndServe(":3000", app)
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

## Using Negroni's `PanicHandlerFunc` Option

Negroni provides an option called `PanicHandlerFunc`, which let you "plug-in" to it's default `Recovery` middleware.

`sentrynegroni` exports a very barebone implementation, which utilizes it, so if you don't need nothing else than just reporting panic's to Sentry,
you can use it instead, as it's just one line of code!

You can still use `BeforeSend` and event processors to modify data before deliverying it to Sentry using this method as well.

```go
app := negroni.New()

recovery := negroni.NewRecovery()
recovery.PanicHandlerFunc = sentrynegroni.PanicHandlerFunc

app.Use(recovery)

mux := http.NewServeMux()
mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
    panic("y tho")
})

app.UseHandler(mux)

http.ListenAndServe(":3000", app)
```
