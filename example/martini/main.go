package main

import (
	sentrymartini "github.com/getsentry/sentry-go/martini"

	"github.com/getsentry/sentry-go"
	"github.com/go-martini/martini"
)

func main() {
	_ = sentry.Init(sentry.ClientOptions{
		Dsn:              "https://363a337c11a64611be4845ad6e24f3ac@sentry.io/297378",
		Debug:            true,
		AttachStacktrace: true,
	})

	m := martini.Classic()
	m.Use(sentrymartini.New(sentrymartini.Options{
		Repanic:         true,
		WaitForDelivery: true,
	}).Handle())

	m.Get("/", func() string {
		panic("y tho")
	})
	m.RunOnAddr(":3000")
}
