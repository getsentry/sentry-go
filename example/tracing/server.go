package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/x/sentryhttp"
)

type Site struct {
	DSN   string
	Pages []Page
}

func NewSite(DSN string) *Site {
	return &Site{DSN: DSN}
}

func (s *Site) Add(p Page) {
	p.Site = s
	s.Pages = append(s.Pages, p)
}

type Page struct {
	Title       string
	Description string
	Path        string
	Handler     func(http.ResponseWriter, *http.Request)
	*Site
}

// func homeHandler(w http.ResponseWriter, r *http.Request) {
// 	tmpl.Execute(w)
// 	fmt.Fprintln(w, `<html>
// <head><title>Sentry Go Example</title></head>
// <body>
// <h1></h1>
// <p>Configured DSN: {}</p>

// </body>
// </html>`)
// }

func server() error {
	const page = `<!DOCTYPE html>
<html lang="en">
  <head>
    <title>{{.Title}}</title>
  </head>
  <body>
    <h1>{{.Title}}</h1>
    <p>Configured DSN: <span>{{or .DSN "not set"}}</span></p>
    <ul>
      <li><a href="/hello/">Send a test message to Sentry</a></li>
      <li><a href="/error/">Send a test error to Sentry</a></li>
      <li><a href="/panic/">Trigger a runtime panic on the server and report to Sentry</a></li>
    </ul>
  </body>
</html>`
	var tmpl = template.Must(template.New("page").Parse(page))

	var dsn = sentry.CurrentHub().Client().Options().Dsn

	/*

	   - Need to track the route, even when handlers/muxes are nested (?)
	   - Transaction name == route (Is there something better? What to do with
	     custom routers/muxes that may have parameters like gorilla/mux?)
	   - op == "http" (?)
	   - then we need to add spans, start with manual spans, within a request
	     handler -- "NewSpanFromContext(ctx, ...)"

	*/

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.HubFromContext(r.Context())
		hub.CaptureMessage("hello")
		tmpl.Execute(w, map[string]interface{}{"DSN": dsn})
	})
	http.HandleFunc("/hello/", func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.HubFromContext(r.Context())
		eventID := hub.CaptureMessage("hello")
		if eventID != nil {
			fmt.Fprintln(w, "Sentry event ID:", *eventID)
		}
	})
	http.HandleFunc("/error/", func(w http.ResponseWriter, r *http.Request) {
		hub := sentry.HubFromContext(r.Context())
		eventID := hub.CaptureException(errors.New("Example error from Go"))
		if eventID != nil {
			fmt.Fprintln(w, "Sentry event ID:", *eventID)
		}
	})
	http.HandleFunc("/panic/", func(w http.ResponseWriter, r *http.Request) {
		panic("Example panic from Go")
	})

	client := http.Client{Transport: sentryhttp.NewTransport(http.DefaultTransport)}

	http.HandleFunc("/transaction/", func(w http.ResponseWriter, r *http.Request) {
		span := sentry.StartSpan(r.Context(), "custom")
		defer span.Finish()
		child1 := span.StartChild("child1")
		time.Sleep(time.Second)
		grandchild1 := child1.StartChild("grandchild1")
		time.Sleep(100 * time.Millisecond)
		grandchild1.Finish()
		child1.Finish()
		child2 := span.StartChild("child2")
		req, _ := http.NewRequestWithContext(child2.Context(), "GET", "/", nil)
		client.Do(req)
		time.Sleep(time.Second)
		child2.Finish()
	})

	s := http.Server{
		Addr:    "127.0.0.1:0",
		Handler: sentryhttp.NewHandler(http.DefaultServeMux),
		BaseContext: func(l net.Listener) context.Context {
			log.Print("Serving http://", l.Addr().String(), "/transaction/")
			return context.Background()
		},
	}
	return s.ListenAndServe()
}
