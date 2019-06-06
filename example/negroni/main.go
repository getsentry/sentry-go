package main

import (
	"encoding/json"
	"fmt"
	"net/http"

	sentrynegroni "github.com/getsentry/sentry-go/negroni"

	"github.com/getsentry/sentry-go"
	"github.com/urfave/negroni"
)

func main() {
	sentry.Init(sentry.ClientOptions{
		Dsn:   "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
		Debug: true,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			pp, _ := json.MarshalIndent(event, "", "  ")
			fmt.Println(string(pp))
			return event
		},
		AttachStacktrace: true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		panic("y tho")
	})

	n := negroni.Classic()
	n.Use(sentrynegroni.New())
	n.UseHandler(mux)

	http.ListenAndServe(":3000", n)
}
