package main

import (
	"fmt"
	"net/http"

	sentrymartini "github.com/getsentry/sentry-go/martini"

	"github.com/getsentry/sentry-go"
	"github.com/go-martini/martini"
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

	app := martini.Classic()

	app.Use(sentrymartini.New(sentrymartini.Options{
		Repanic: true,
	}))

	app.Use(func(rw http.ResponseWriter, r *http.Request, c martini.Context, hub *sentry.Hub) {
		hub.Scope().SetTag("someRandomTag", "maybeYouNeedIt")
	})

	app.Get("/", func(rw http.ResponseWriter, r *http.Request, hub *sentry.Hub) {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("unwantedQuery", "someQueryDataMaybe")
			hub.CaptureMessage("User provided unwanted query string, but we recovered just fine")
		})
		rw.WriteHeader(http.StatusOK)
	})

	app.Get("/foo", func() string {
		// sentrymartini handler will catch it just fine, and because we attached "someRandomTag"
		// in the middleware before, it will be sent through as well
		panic("y tho")
	})

	app.RunOnAddr(":3000")
}
