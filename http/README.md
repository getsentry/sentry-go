<p align="center">
  <a href="https://sentry.io" target="_blank" align="center">
    <img src="https://sentry-brand.storage.googleapis.com/sentry-logo-black.png" width="280">
  </a>
  <br />
</p>

# Official Sentry net/http Handler for Sentry-go SDK

**go.dev:** https://pkg.go.dev/github.com/getsentry/sentry-go/http

**Example:** https://github.com/getsentry/sentry-go/tree/master/_examples/http

## Installation

```sh
go get github.com/getsentry/sentry-go/http
```

```go
import (
    "fmt"
    "net/http"

    "github.com/getsentry/sentry-go"
    sentryhttp "github.com/getsentry/sentry-go/http"
)

// To initialize Sentry's handler, you need to initialize Sentry itself beforehand
if err := sentry.Init(sentry.ClientOptions{
    Dsn: "your-public-dsn",
}); err != nil {
    fmt.Printf("Sentry initialization failed: %v\n", err)
}

// Create an instance of sentryhttp
sentryHandler := sentryhttp.New(sentryhttp.Options{})

// Once it's done, you can setup routes and attach the handler as one of your middleware
http.Handle("/", sentryHandler.Handle(&handler{}))
http.HandleFunc("/foo", sentryHandler.HandleFunc(func(rw http.ResponseWriter, r *http.Request) {
    panic("y tho")
}))

fmt.Println("Listening and serving HTTP on :3000")

// And run it
if err := http.ListenAndServe(":3000", nil); err != nil {
    panic(err)
}
```

## Configuration

`sentryhttp` accepts a struct of `Options` that allows you to configure how the handler will behave.

Currently it respects 3 options:

```go
// Whether Sentry should repanic after recovery, in most cases it should be set to true,
// and you should gracefully handle http responses.
Repanic         bool
// Whether you want to block the request before moving forward with the response.
// Useful, when you want to restart the process after it panics.
WaitForDelivery bool
// Timeout for the event delivery requests.
Timeout         time.Duration
```

## Usage

`sentryhttp` attaches a request-specific `*sentry.Scope` and transaction to the request context. Pass `r.Context()` to capture functions such as `sentry.CaptureMessage` and `sentry.CaptureException` so request data, custom scope data, and trace information are applied to the event.
Use `sentry.ScopeFromContext(r.Context())` when you need to add data that should be available to captures made during the request.

**Keep in mind that the request scope won't be available in middleware attached before `sentryhttp`!**

```go
type handler struct{}

func (h *handler) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	scope := sentry.ScopeFromContext(ctx)
	scope.SetTag("unwantedQuery", "someQueryDataMaybe")
	sentry.CaptureMessage(ctx, "User provided unwanted query string, but we recovered just fine")
	rw.WriteHeader(http.StatusOK)
}

func enhanceSentryEvent(handler http.HandlerFunc) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		sentry.ScopeFromContext(r.Context()).SetTag("someRandomTag", "maybeYouNeedIt")
		handler(rw, r)
	}
}

// Later in the code

sentryHandler := sentryhttp.New(sentryhttp.Options{
    Repanic: true,
})

http.Handle("/", sentryHandler.Handle(&handler{}))
http.HandleFunc("/foo", sentryHandler.HandleFunc(
    enhanceSentryEvent(func(rw http.ResponseWriter, r *http.Request) {
        panic("y tho")
    }),
))

fmt.Println("Listening and serving HTTP on :3000")

if err := http.ListenAndServe(":3000", nil); err != nil {
    panic(err)
}
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
