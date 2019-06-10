package main

import (
	"net/http"

	sentrynegroni "github.com/getsentry/sentry-go/negroni"

	"github.com/getsentry/sentry-go"
	"github.com/urfave/negroni"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
		Debug:            true,
		AttachStacktrace: true,
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		panic("y tho")
	})

	n := negroni.Classic()
	n.Use(sentrynegroni.New(sentrynegroni.Options{
		Repanic:         true,
		WaitForDelivery: true,
	}))
	n.UseHandler(mux)

	_ = http.ListenAndServe(":3000", n)
}
